package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
	targetIndexFile       = "/home/sormazabal/src/SALTO/data/general-service/general-service-index.json"
	jsonFilePath          = "/home/sormazabal/src/SALTO/update_status.json"
	generateRandomFolder  = false
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

	// Ensure `globalServerInstance` is set
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
	newBinaryPath := "/home/sormazabal/src/SALTO/tmp/general-service.zip"
	destinationPath := "/home/sormazabal/src/SALTO/general-service.zip"

	// Generating the temporary folder

	// get working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	tmpDir := filepath.Join(cwd, "tmp")
	// create a temporary folder for storing the demo artifacts
	os.Mkdir(tmpDir, 0750)

	var data map[string]indexInfo

	fmt.Printf("The index file is located in: %s \n", targetIndexFile)

	// Read the actual JSON file content
	fileContent, err := os.ReadFile(targetIndexFile)
	if err != nil {
		return fmt.Errorf("failed to read index file: %w", err)
	}

	// Parse JSON into the map
	err = json.Unmarshal(fileContent, &data)
	if err != nil {
		fmt.Printf("\U0001F534Error parsing JSON: %v\U0001F534", err)
		return err
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

	err = verifyingDownloadedFile(targetIndexFile, newBinaryPath)

	if err == nil {
		// Replace old binary
		err = os.Rename(newBinaryPath, destinationPath)
		if err != nil {
			return fmt.Errorf("failed to replace binary: %w", err)
		}
	}

	// Calling the upgrader
	cmd := exec.Command("./upgrader")

	// Attach the output to the console
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start the new process
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to execute the upgrader: %w", err)
	}
	// Telling that the Update has been successful.
	fmt.Println("✅ Update successful. Shutting down server...")

	// handleShutdown waits for a termination signal and shuts down the server
	syscall.Kill(syscall.Getpid(), syscall.SIGINT)
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

// Verifying a file
func verifyingDownloadedFile(targetIndexFile, DonwloadedFilePath string) error {

	var data map[string]indexInfo

	// Read the actual JSON file content
	fileContent, err := os.ReadFile(targetIndexFile)
	if err != nil {
		return fmt.Errorf("failed to read index file: %w", err)
	}

	// Parse JSON into the map
	err = json.Unmarshal(fileContent, &data)
	if err != nil {
		fmt.Printf("\U0001F534Error parsing JSON: %v\U0001F534", err)
		return err
	}

	indexHash := data[service].Hashes.Sha256

	fmt.Printf("\nThe hash from the nebula-service-index.json is %s", indexHash)

	// Computing the hash of the downloaded file

	// Compute the SHA256 hash
	downloadedFilehash, err := ComputeSHA256(DonwloadedFilePath)

	fmt.Printf("Downloaded file hash is: %s\n", downloadedFilehash)

	if err != nil {
		fmt.Printf("\U0001F534Error computing hash: %v\U0001F534\n", err)
		return fmt.Errorf("error while computing the hash")
	}

	if indexHash == downloadedFilehash {
		fmt.Printf("\U0001F7E2The target file has been downloaded successfully!\U0001F7E2\n")
	} else {
		return fmt.Errorf("there has been an error while downloading the file, the hashes do not match")
	}
	return nil
}

// Computing the SHA256 of a file
func ComputeSHA256(filePath string) (string, error) {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create a SHA256 hash object
	hasher := sha256.New()

	// Copy the file contents into the hasher
	// This reads the file in chunks to handle large files efficiently
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to compute hash: %w", err)
	}

	// Get the final hash as a byte slice and convert to a hexadecimal string
	hash := hasher.Sum(nil)
	return fmt.Sprintf("%x", hash), nil
}

// Periodic Update Check (Runs in Background)
func periodicUpdateCheck(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
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

// Gracefully shutdown the server
func (s *Server) Shutdown() {
	fmt.Println("🛑 Shutting down server...")
	s.cancel()
	time.Sleep(1 * time.Second) // Allow cleanup
	fmt.Println("✅ Server stopped.")
}
