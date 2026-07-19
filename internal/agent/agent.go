package agent

// This should really be called Orchestrator or Coordinator imo.
// It is the general manager of ONE NODE in this system.

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"github.com/hashicorp/raft"
	"io"
	"net"
	"sync"
	"time"

	"github.com/soheilhy/cmux"
	"github.com/wintercoder1/golog/internal/auth"
	"github.com/wintercoder1/golog/internal/discovery"
	"github.com/wintercoder1/golog/internal/log"
	"github.com/wintercoder1/golog/internal/server"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Agent should be Coordinator imo.
type Agent struct {
	Config Config

	mux        cmux.CMux
	log        *log.DistributedLog   // The actual log itself -- Now Distributed!! -- Important
	server     *grpc.Server          // The server that makes the log accessible
	membership *discovery.Membership // The membership access list abstraction
	//replicator *log.Replicator       // Replicator logic abstracted away separately from the log itself. Communicates with channels.
	// Replication now done infinitely better with Raft.

	shutdown     bool
	shutdowns    chan struct{}
	shutdownLock sync.Mutex
}

type Config struct {
	ServerTLSConfig *tls.Config
	PeerTLSConfig   *tls.Config

	DataDir        string
	BindAddr       string
	RPCPort        int
	NodeName       string
	StartJoinAddrs []string
	ACLModelFile   string
	ACLPolicyFile  string
	Bootstrap      bool // Toggle to enable bootstraping a Raft cluster.
}

func (c Config) RPCAddr() (string, error) {
	host, _, err := net.SplitHostPort(c.BindAddr)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", host, c.RPCPort), nil
}

func New(config Config) (*Agent, error) {
	a := &Agent{
		Config:    config,
		shutdowns: make(chan struct{}),
	}
	setup := []func() error{
		a.setupLogger,
		a.setupMux,
		a.setupLog,
		a.setupServer,
		a.setupServe,
		a.setupMembership,
	}
	for _, fn := range setup {
		if err := fn(); err != nil {
			// Clean up resources from any previously successful setup steps
			_ = a.Shutdown()
			return nil, err
		}
	}
	return a, nil
}

// Creates a listener on thr RPC address that will accept both Raft and gRPC connections.
// Will make a mux with that listener.
func (a *Agent) setupMux() error {
	addr, err := net.ResolveTCPAddr("tcp", a.Config.BindAddr)
	if err != nil {
		return err
	}
	rpcAddr := fmt.Sprintf(
		"%s:%d",
		addr.IP.String(),
		a.Config.RPCPort,
	)
	ln, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return err
	}
	a.mux = cmux.New(ln)
	return nil
}

//func (a *Agent) setupMux() error {
//	rpcAddr := fmt.Sprintf("0.0.0.0:%d", a.Config.RPCPort)
//
//	ln, err := net.Listen("tcp", rpcAddr)
//	if err != nil {
//		return fmt.Errorf("listen on %s: %w", rpcAddr, err)
//	}
//
//	a.mux = cmux.New(ln)
//	return nil
//}

func (a *Agent) setupLogger() error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	zap.ReplaceGlobals(logger)
	return nil
}

func (a *Agent) setupLog() error {
	raftLn := a.mux.Match(func(reader io.Reader) bool {
		b := make([]byte, 1)
		if _, err := reader.Read(b); err != nil {
			return false
		}
		return bytes.Compare(b, []byte{byte(log.RaftRPC)}) == 0
	})
	logConfig := log.Config{}
	logConfig.Raft.StreamLayer = log.NewStreamLayer(
		raftLn,
		a.Config.ServerTLSConfig,
		a.Config.PeerTLSConfig,
	)
	rpcAddr, err := a.Config.RPCAddr()
	if err != nil {
		return err
	}
	logConfig.Raft.BindAddr = rpcAddr
	logConfig.Raft.LocalID = raft.ServerID(a.Config.NodeName)
	logConfig.Raft.Bootstrap = a.Config.Bootstrap
	//var err error
	a.log, err = log.NewDistributedLog(
		a.Config.DataDir,
		logConfig,
	)
	if err != nil {
		return err
	}
	if a.Config.Bootstrap {
		err = a.log.WaitForLeader(3 * time.Second)
	}
	return err
}

//func (a *Agent) setupLog() error {
//	var err error
//	a.log, err = log.NewLog(
//		a.Config.DataDir,
//		log.Config{},
//	)
//	return err
//}

func (a *Agent) setupServer() error {
	authorizer := auth.New(
		a.Config.ACLModelFile,
		a.Config.ACLPolicyFile,
	)
	serverConfig := &server.Config{
		CommitLog:   a.log,
		Authorizer:  authorizer,
		GetServerer: a.log,
	}
	var opts []grpc.ServerOption
	if a.Config.ServerTLSConfig != nil {
		creds := credentials.NewTLS(a.Config.ServerTLSConfig)
		opts = append(opts, grpc.Creds(creds))
	}
	var err error
	a.server, err = server.NewGRPCServer(serverConfig, opts...)
	if err != nil {
		return err
	}
	grpcLn := a.mux.Match(cmux.Any())
	go func() {
		if err := a.server.Serve(grpcLn); err != nil {
			_ = a.Shutdown()
		}
	}()
	return err
}

func (a *Agent) setupServe() error {
	go func() {
		if err := a.mux.Serve(); err != nil {
			if !a.shutdown {
				zap.L().Error("mux failed", zap.Error(err))
				_ = a.Shutdown()
			}
		}
	}()
	return nil
}

func (a *Agent) setupMembership() error {
	rpcAddr, err := a.Config.RPCAddr()
	if err != nil {
		return err
	}
	//
	a.membership, err = discovery.New(a.log, discovery.Config{
		NodeName: a.Config.NodeName,
		BindAddr: a.Config.BindAddr,
		Tags: map[string]string{
			"rpc_addr": rpcAddr,
		},
		StartJoinAddrs: a.Config.StartJoinAddrs,
	})
	return err
}

func (a *Agent) Shutdown() error {
	a.shutdownLock.Lock()
	defer a.shutdownLock.Unlock()
	if a.shutdown {
		return nil
	}
	a.shutdown = true
	close(a.shutdowns)

	// Add nil checks to prevent panics during partial initialization
	shutdown := []func() error{
		func() error {
			if a.membership != nil {
				return a.membership.Leave()
			}
			return nil
		},
		//func() error {
		//	if a.replicator != nil {
		//		return a.replicator.Close()
		//	}
		//	return nil
		//},
		func() error {
			if a.server != nil {
				a.server.GracefulStop()
			}
			return nil
		},
		func() error {
			if a.log != nil {
				return a.log.Close()
			}
			return nil
		},
	}
	for _, fn := range shutdown {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) serve() error {
	if err := a.mux.Serve(); err != nil {
		_ = a.Shutdown()
		return err
	}
	return nil
}
