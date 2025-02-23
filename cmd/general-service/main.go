package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/peterbourgon/ff/v4/ffyaml"
	sdlog "github.com/saltosystems-internal/x/log/stackdriver"
	"github.com/sorayaormazabalmayo/general-service/internal/cli"
)

const jsonFilePath = "/var/lib/your-app/update_status.json"

var updateStatus = struct {
	UpdateAvailable int `json:"update_available"`
}{UpdateAvailable: 0} // Default: No update

// Read update status from JSON file
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

// API handler that serves JSON update status
func checkUpdateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updateStatus)
}

// API handler that executes a bash script when update button is clicked
func runUpdateHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("⚙️ Running update script...")

	// Define the script path
	scriptPath := "/var/lib/your-app/update.sh"

	// Execute the script
	cmd := exec.Command("/bin/bash", scriptPath)
	err := cmd.Run()

	if err != nil {
		fmt.Fprintf(w, `{"success": false, "error": "%v"}`, err)
	} else {
		fmt.Fprint(w, `{"success": true}`)
	}

	// Reset update status after running
	updateStatus.UpdateAvailable = 0
	writeUpdateStatus()
}

// Periodically check for updates
func periodicUpdateCheck() {
	for {
		time.Sleep(5 * time.Minute)

		// Read update status from file
		readUpdateStatus()

		// Simulating update detection (replace with actual logic)
		if updateStatus.UpdateAvailable == 1 {
			fmt.Println("🔄 Update available! Notifying frontend.")
		}
	}
}

func main() {
	// Create new logger
	logger := sdlog.New()

	// Read update status at startup
	readUpdateStatus()

	// Start API server in a goroutine
	go func() {
		http.HandleFunc("/check-update", checkUpdateHandler)
		http.HandleFunc("/run-update", runUpdateHandler)
		fmt.Println("🚀 API running on http://localhost:8080")
		http.ListenAndServe(":8080", nil)
	}()

	// Start periodic update checker
	go periodicUpdateCheck()

	// Create command
	generalServiceCmd := cli.NewGeneralServiceCommand(logger)

	// control aspects of parsing behaviour
	opts := []ff.Option{
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ffyaml.Parse),
	}

	// Run CLI command
	if err := generalServiceCmd.ParseAndRun(context.Background(), os.Args[1:], opts...); err != nil {
		if errors.Is(err, ff.ErrHelp) || errors.Is(err, ff.ErrDuplicateFlag) || errors.Is(err, ff.ErrAlreadyParsed) || errors.Is(err, ff.ErrUnknownFlag) || errors.Is(err, ff.ErrNotParsed) {
			fmt.Fprintf(os.Stderr, "\n%s\n", ffhelp.Command(&generalServiceCmd))
		}

		if !errors.Is(err, ff.ErrHelp) {
			logger.Error(err)
		}
		os.Exit(1)
	}
}
