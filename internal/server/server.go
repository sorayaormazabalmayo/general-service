package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/saltosystems-internal/x/log"
	pkgserver "github.com/saltosystems-internal/x/server"
)

// Server is a meta-server composed of a gRPC server and an HTTP server.
type Server struct {
	s      *pkgserver.GroupServer
	logger log.Logger
	cancel context.CancelFunc // To stop background tasks when shutting down
}

// Update status JSON file path
const jsonFilePath = "/home/sormazabal/src/general-service/update_status.json"

// Struct to store update status
var updateStatus = struct {
	UpdateAvailable int `json:"update_available"`
}{UpdateAvailable: 0} // Default: No update

// Read update status from file
func readUpdateStatus() {
	file, err := os.ReadFile(jsonFilePath)
	if err != nil {
		fmt.Println("⚠️ Could not read update status file, using default (0)")
		return
	}
	err = json.Unmarshal(file, &updateStatus)
	if err != nil {
		fmt.Println("⚠️ Could not parse update status JSON, using default (0)")
	}
}

// Write update status to JSON file
func writeUpdateStatus() {
	file, err := json.MarshalIndent(updateStatus, "", "  ")
	if err != nil {
		fmt.Println("❌ Error marshalling update status:", err)
		return
	}
	err = os.WriteFile(jsonFilePath, file, 0644)
	if err != nil {
		fmt.Println("❌ Error writing update status file:", err)
	}
}

// API Handler: Check update status
func checkUpdateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // Explicitly set 200 OK
	json.NewEncoder(w).Encode(updateStatus)
}

// API Handler: Apply Update (Runs Bash Script)
func runUpdateHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("⚙️ Running update script...")
	scriptPath := "/var/lib/your-app/update.sh"

	cmd := exec.Command("/bin/bash", scriptPath)
	err := cmd.Run()

	response := map[string]interface{}{
		"success": err == nil,
	}
	if err != nil {
		response["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	// Reset update status after running
	updateStatus.UpdateAvailable = 0
	writeUpdateStatus()
}

// Periodic Update Check (Runs in Background)
func periodicUpdateCheck(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			readUpdateStatus()
			if updateStatus.UpdateAvailable == 1 {
				fmt.Println("🔄 Update available! Notifying frontend.")
			}
		case <-ctx.Done():
			fmt.Println("🛑 Stopping periodic update check...")
			return
		}
	}
}

// CORS Middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// NewServer creates a new general-service server with HTTP & API
func NewServer(cfg *Config, logger log.Logger) (*Server, error) {
	var (
		servers        []pkgserver.Server
		httpServerOpts []pkgserver.HTTPServerOption
	)

	// Validate config
	if cfg.HTTPAddr == "" {
		return nil, errors.New("invalid config: HTTPAddr missing")
	}

	// Create HTTP Multiplexer
	mux := http.NewServeMux()

	// Serve static files from the "static" directory
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Serve index.html (Home Page)
	mux.HandleFunc("/nebula", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		http.ServeFile(w, r, "static/index.html")
	})

	// 🌟 Register Update API Routes Inside `mux` 🌟
	mux.HandleFunc("/check-update", checkUpdateHandler)
	mux.HandleFunc("/run-update", runUpdateHandler)

	// Apply CORS middleware
	wrappedMux := corsMiddleware(mux)

	// Start periodic update checker in a separate goroutine with proper cancellation
	ctx, cancel := context.WithCancel(context.Background())
	go periodicUpdateCheck(ctx) // ✅ Run in a goroutine to avoid blocking server startup

	// Start HTTP server with routes
	httpServerOpts = append(httpServerOpts, pkgserver.WithRoutes(
		&pkgserver.Route{Pattern: "/", Handler: wrappedMux},
	))
	httpServer, err := pkgserver.NewHTTPServer(cfg.HTTPAddr, httpServerOpts...)
	if err != nil {
		cancel() // Ensure cleanup if server fails
		return nil, err
	}
	servers = append(servers, httpServer)

	s, err := pkgserver.NewGroupServer(context.Background(), pkgserver.WithServers(servers))
	if err != nil {
		cancel() // Ensure cleanup if server fails
		return nil, err
	}

	return &Server{s: s, logger: logger, cancel: cancel}, nil
}

// Run runs the meta-server
func (s *Server) Run() error {
	fmt.Println("🚀 Server started...")
	err := s.s.Run(context.Background())
	if err != nil {
		fmt.Println("❌ Server error:", err)
	}
	return err
}

// Shutdown stops the server and cleans up background processes
func (s *Server) Shutdown() {
	fmt.Println("🛑 Shutting down server...")
	s.cancel() // Stop periodic update check
}
