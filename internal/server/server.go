package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/saltosystems-internal/x/log"
	pkgserver "github.com/saltosystems-internal/x/server"
)

// Server is a meta-server composed by a grpc server and a http server
type Server struct {
	s      *pkgserver.GroupServer
	logger log.Logger
}

// NewServer creates a new sns server which consist of a grpc server, a
// http server and an additional http server for administration
func NewServer(cfg *Config, logger log.Logger) (*Server, error) {
	var (
		servers        []pkgserver.Server
		httpServerOpts []pkgserver.HTTPServerOption
	)

	// check config validity
	if !cfg.Valid() {
		return nil, errors.New("invalid config")
	}

	// http-server
	mux := http.NewServeMux()

	// Serve static files from the "static" directory
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Serve index.html
	mux.HandleFunc("/nebula", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		http.ServeFile(w, r, "static/index.html")
	})

	httpServerOpts = append(httpServerOpts, pkgserver.WithRoutes(
		&pkgserver.Route{Pattern: "/", Handler: mux},
	))
	httpServer, err := pkgserver.NewHTTPServer(cfg.HTTPAddr, httpServerOpts...)
	if err != nil {
		return nil, err
	}
	servers = append(servers, httpServer)

	s, err := pkgserver.NewGroupServer(context.Background(), pkgserver.WithServers(servers))
	if err != nil {
		return nil, err
	}

	return &Server{
		s:      s,
		logger: logger,
	}, nil
}

// Run runs the meta-server
func (s *Server) Run() error {
	return s.s.Run(context.Background())
}
