package server

import (
	"encoding/json"
	"github.com/gorilla/mux"
	api "github.com/wintercoder1/golog/api/v1"
	"net/http"
	//api "github.com/wintercoder1/golog/api/v1"
	log "github.com/wintercoder1/golog/internal/log"
)

func NewHttpServer(addr string) *http.Server {
	httpsrv := newHTTPServer()
	r := mux.NewRouter()
	r.HandleFunc("/", httpsrv.handleProduce).Methods("POST")
	r.HandleFunc("/", httpsrv.handleConsume).Methods("GET")
	return &http.Server{
		Addr:    addr,
		Handler: r,
	}
}

type httpServer struct {
	Log *log.Log
}

func newHTTPServer() *httpServer {
	log, err := log.NewLog("", log.Config{})
	if err != nil {
		return nil
	}
	return &httpServer{
		Log: log,
	}
}

//type ProduceRequest struct {
//	Record api.Record `json:"record"`
//}
//
//type ProduceResponse struct {
//	Offset uint64 `json:"offset""`
//}
//
//type ConsumeRequest struct {
//	Offset uint64 `json:"offset"`
//}
//
//type ConsumeResponse struct {
//	Record Record `json:"record"`
//}

func (s *httpServer) handleProduce(w http.ResponseWriter, r *http.Request) {
	// Read request
	var req api.ProduceRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Append to log
	off, err := s.Log.Append(req.Record)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// return response
	res := api.ProduceResponse{Offset: off}
	err = json.NewEncoder(w).Encode(&res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *httpServer) handleConsume(w http.ResponseWriter, r *http.Request) {
	// Read request
	var req api.ConsumeRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Read from log
	record, err := s.Log.Read(req.Offset)
	if err == ErrOffsetNotFound { // Offset not found error is more specific thus its own handling
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// return response
	res := api.ConsumeResponse{Record: record}
	err = json.NewEncoder(w).Encode(&res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
