package server

import (
	"context"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"

	api "github.com/wintercoder1/golog/api/v1"
	"time"

	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	peer2 "google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type Config struct {
	CommitLog   CommitLog
	Authorizer  Authorizer
	GetServerer GetServerer
}

const (
	objectWildcard = "*"
	produceAction  = "produce"
	consumeAction  = "consume"
)

var _ api.LogServer = (*grpcServer)(nil)

type grpcServer struct {
	*api.UnimplementedLogServer
	*Config
}

// Wraps newgrpcServer.
func NewGRPCServer(config *Config, opts ...grpc.ServerOption) (*grpc.Server, error) {
	// Logging options
	// Will wrap grpc with them
	logger := zap.L().Named("server")
	zapOpts := []grpc_zap.Option{
		grpc_zap.WithDurationField(
			func(duration time.Duration) zapcore.Field {
				return zap.Int64(
					"grpc.time_ns",
					duration.Nanoseconds(),
				)
			},
		),
	}
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	err := view.Register(ocgrpc.DefaultServerViews...)
	if err != nil {
		return nil, err
	}
	// Configure gRPC to apply Zap interceptors that log the grpc and attach
	// OpenCensus as the server's stat handler so that OpenCensus can record.
	opts = append(opts,
		grpc.StreamInterceptor(
			grpc_middleware.ChainStreamServer(
				grpc_ctxtags.StreamServerInterceptor(),
				grpc_zap.StreamServerInterceptor(logger, zapOpts...),
				grpc_auth.StreamServerInterceptor(authenticate),
			),
		),
		grpc.UnaryInterceptor(
			grpc_middleware.ChainUnaryServer(
				grpc_ctxtags.UnaryServerInterceptor(),
				grpc_zap.UnaryServerInterceptor(logger, zapOpts...),
				grpc_auth.UnaryServerInterceptor(authenticate),
			),
		),
		grpc.StatsHandler(&ocgrpc.ServerHandler{}),
	)
	//
	gsrv := grpc.NewServer(opts...)

	hsrv := health.NewServer()
	hsrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gsrv, hsrv)

	srv, err := newgrpcServer(config)
	if err != nil {
		return nil, err
	}
	api.RegisterLogServer(gsrv, srv)
	return gsrv, nil
}

//func NewGRPCServer(config *Config, opts ...grpc.ServerOption) (*grpc.Server, error) {
//	opts = append(opts, grpc.StreamInterceptor(
//		grpcmiddleware.ChainStreamServer(
//			grpcauth.StreamServerInterceptor(authenticate),
//		)),
//		grpc.UnaryInterceptor(grpcmiddleware.ChainUnaryServer(
//			grpcauth.UnaryServerInterceptor(authenticate),
//		),
//		))
//	gsrv := grpc.NewServer(opts...)
//	srv, err := newgrpcServer(config)
//	if err != nil {
//		return nil, err
//	}
//	api.RegisterLogServer(gsrv, srv)
//	return gsrv, nil
//}

//func NewGRPCServer(config *Config) (*grpc.Server, error) {
//	gsrv := grpc.NewServer()
//	srv, err := newgrpcServer(config)
//	if err != nil {
//		return nil, err
//	}
//	api.RegisterLogServer(gsrv, srv)
//	return gsrv, nil
//}

func newgrpcServer(config *Config) (srv *grpcServer, err error) {
	srv = &grpcServer{
		Config: config,
	}
	return srv, err
}

func (s *grpcServer) Produce(ctx context.Context, req *api.ProduceRequest) (*api.ProduceResponse, error) {
	// Auth Code
	if err := s.Authorizer.Authorize(
		subject(ctx),
		objectWildcard,
		produceAction,
	); err != nil {
		return nil, err
	}
	// Once auth is complete:
	// Produce - which is an append to the log.
	offset, err := s.CommitLog.Append(req.Record)
	if err != nil {
		return nil, err
	}
	return &api.ProduceResponse{Offset: offset}, nil
}

func (s *grpcServer) Consume(ctx context.Context, req *api.ConsumeRequest) (*api.ConsumeResponse, error) {
	// Auth Code
	if err := s.Authorizer.Authorize(
		subject(ctx),
		objectWildcard,
		consumeAction,
	); err != nil {
		return nil, err
	}
	// Once auth is complete:
	// Read -- which is random access with the segment index. Not only the end.
	record, err := s.CommitLog.Read(req.Offset)
	if err != nil {
		return nil, err
	}
	return &api.ConsumeResponse{Record: record}, nil
}

func (s *grpcServer) ProduceStream(stream api.Log_ProduceStreamServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		res, err := s.Produce(stream.Context(), req)
		if err != nil {
			return err
		}
		if err = stream.Send(res); err != nil {
			return err
		}
	}
}

func (s *grpcServer) ConsumeStream(req *api.ConsumeRequest, stream api.Log_ConsumeStreamServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return nil
		default:
			res, err := s.Consume(stream.Context(), req)
			switch err.(type) {
			case nil:
			case api.ErrOffsetOutOfRange:
				continue
			default:
				return err
			}
			if err = stream.Send(res); err != nil {
				return err
			}
			req.Offset++
		}
	}
}

func (s *grpcServer) GetServers(ctx context.Context, req *api.GetServersRequest) (*api.GetServersResponse, error) {
	servers, err := s.GetServerer.GetServers()
	if err != nil {
		return nil, err
	}
	return &api.GetServersResponse{
		Servers: servers,
	}, nil
}

type GetServerer interface {
	GetServers() ([]*api.Server, error)
}

//
//

type CommitLog interface {
	Append(record *api.Record) (uint64, error)
	Read(uint64) (*api.Record, error)
}

type Authorizer interface {
	Authorize(subject, object, action string) error
}

func authenticate(ctx context.Context) (context.Context, error) {
	method, ok := grpc.Method(ctx)
	if ok && method == "/grpc.health.v1.Health/Check" {
		return context.WithValue(ctx, subjectContextKey{}, ""), nil
	}

	peer, ok := peer2.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "peer information unavailable")
	}

	if peer.AuthInfo == nil {
		return context.WithValue(ctx, subjectContextKey{}, ""), nil
	}

	tlsInfo, ok := peer.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unexpected authentication type")
	}

	if len(tlsInfo.State.VerifiedChains) == 0 ||
		len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return nil, status.Error(codes.Unauthenticated, "client certificate not verified")
	}

	subject := tlsInfo.State.VerifiedChains[0][0].Subject.CommonName
	return context.WithValue(ctx, subjectContextKey{}, subject), nil
}

//func authenticate(ctx context.Context) (context.Context, error) {
//	peer, ok := peer2.FromContext(ctx)
//	// Couldn't create context
//	if !ok {
//		return ctx, status.New(
//			codes.Unknown,
//			"couldn't find peer info",
//		).Err()
//	}
//	// No auth info
//	if peer.AuthInfo == nil {
//		return context.WithValue(ctx, subjectContextKey{}, ""), nil
//	}
//	// tls info and subject wil lbe added to context and returned.
//	tlsInfo := peer.AuthInfo.(credentials.TLSInfo)
//	subject := tlsInfo.State.VerifiedChains[0][0].Subject.CommonName
//	ctx = context.WithValue(ctx, subjectContextKey{}, subject)
//	return ctx, nil
//}

func subject(ctx context.Context) string {
	return ctx.Value(subjectContextKey{}).(string)
}

type subjectContextKey struct{}
