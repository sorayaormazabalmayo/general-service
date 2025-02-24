package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/saltosystems-internal/x/log"
	pkgserver "github.com/saltosystems-internal/x/server"
	"golang.org/x/oauth2/google"
)

// Server is a meta-server composed of a gRPC server and an HTTP server.
type Server struct {
	s      *pkgserver.GroupServer
	logger log.Logger
	cancel context.CancelFunc // To stop background tasks when shutting down
}

type indexInfo struct {
	Bytes  string `json:"bytes"`
	Path   string `json:"path"`
	Hashes struct {
		Sha256 string `json:"sha256"`
	} `json:"hashes"`
	Version     string `json:"version"`
	ReleaseDate string `json:"release-date"`
}

// Struct to store update status
var updateStatus = struct {
	UpdateAvailable int `json:"update_available"`
}{UpdateAvailable: 0} // Default: No update

var (
	serviceAccountKeyPath = "/home/sormazabal/artifact-downloader-key.json"
	service               = "general-service"
	targetIndexFile       = filepath.Join("data", service, fmt.Sprintf("%s-index.json", service))
	jsonFilePath          = "/home/sormazabal/src/general-service/update_status.json"
)

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

func runUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { // ✅ Ensure it's a POST request
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Println("⚙️ Running update process...")

	err := performUpdate() // ✅ Run the update directly in Go
	if err != nil {
		response := map[string]interface{}{"success": false, "error": err.Error()}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true}) // ✅ Ensure valid JSON response
}

func performUpdate() error {
	// Download new binary, verify, replace old binary, restart service
	newBinaryPath := "opt/SALTO/tmp/general-service"
	destinationPath := "/opt/your-app/general-service"

	var data map[string]indexInfo

	// Parse JSON into the map
	err := json.Unmarshal([]byte(targetIndexFile), &data)
	if err != nil {
		fmt.Printf("\U0001F534Error parsing JSON: %v\U0001F534", err)
	}

	// Getting service path
	servicePath := data[service].Path

	// Download the artifact without specifying the file type
	err = downloadArtifact(serviceAccountKeyPath, servicePath, newBinaryPath)
	if err != nil {
		fmt.Printf("\U0001F534Failed to download binary: %v\U0001F534\n", err)
		os.Exit(1)
	}

	// Make sure the new binary is executable
	err = os.Chmod(newBinaryPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to set executable permissions: %w", err)
	}

	//*

	// Replace old binary
	err = os.Rename(newBinaryPath, destinationPath)
	if err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	// Restart the application (or notify an external service manager)
	return err

}

// Downloading the artifact

func downloadArtifact(serviceAccountKeyPath, servicePath, newBinaryPath string) error {
	// Authenticate using the service account key
	ctx := context.Background()
	creds, err := google.CredentialsFromJSON(ctx, readFile(serviceAccountKeyPath), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return fmt.Errorf("failed to load service account credentials: %w", err)
	}

	// Create HTTP client with the token
	client := &http.Client{}
	req, err := http.NewRequest("GET", servicePath, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add Authorization header with Bearer token
	token, err := creds.TokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to retrieve token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	// Perform the request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download artifact, status code: %d", resp.StatusCode)
	}

	// Determine the file name from the Content-Disposition header or use a default name
	contentDisposition := resp.Header.Get("Content-Disposition")
	fileName := newBinaryPath
	if contentDisposition != "" {
		_, params, err := mime.ParseMediaType(contentDisposition)
		if err == nil {
			if name, ok := params["filename"]; ok {
				fileName = name
			}
		}
	}
	fmt.Printf("Saving file as: %s\n", fileName)

	// Write the response to a file
	out, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// readFile reads the content of the service account key JSON file
func readFile(path string) []byte {
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("\U0001F534Error reading file %s: %v\U0001F534\n", path, err)
		os.Exit(1)
	}
	return content
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
	// 🌟 Register Update API Routes Inside mux 🌟
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
