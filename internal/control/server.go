package control

import (
	"context"
	"encoding/json"
	"net"
	"net/http"

	"github.com/MaimoryLab/codex-server/internal/diagnostics"
)

type StatusProvider func(context.Context) diagnostics.Snapshot

type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

func New(status StatusProvider) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(status(r.Context())); err != nil {
			http.Error(w, "encode status: "+err.Error(), http.StatusInternalServerError)
		}
	})
	return &Server{httpServer: &http.Server{Handler: mux}}
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.listener = listener
	go func() {
		_ = s.httpServer.Serve(listener)
	}()
	return nil
}

func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return "http://" + s.listener.Addr().String()
}

func (s *Server) Close(ctx context.Context) error {
	if s.listener == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}
