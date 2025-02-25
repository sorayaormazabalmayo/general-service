package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Struct to store update status
type UpdateStatus struct {
	UpdateAvailable int `json:"update_available"`
}

var (
	// Path to the JSON file
	jsonFilePath = "update_status.json"
)

func main() {

	// Unzipping the donwloaded file

	zipPath := "/home/sormazabal/src/general-service/tmp/general-service.zip"
	unzipPth := "/home/sormazabal/src/general-service/tmp"

	Unzip(zipPath, unzipPth)

	// Creating a /previous foler
	os.Mkdir("previous", 0750)

	// Moving general-service, /static and /config to previous

	// Files and folders to move
	itemsToMove := []string{
		"/home/sormazabal/src/general-service/general-service",
		"/home/sormazabal/src/general-service/general-service.yml",
		"/home/sormazabal/src/general-service/static",
	}

	// Target directory
	targetDir := "/home/sormazabal/src/general-service/previous"

	// Move files and folders
	err := moveContentsToDir(itemsToMove, targetDir)
	if err != nil {
		fmt.Println("❌ Error:", err)
	} else {
		fmt.Println("✅ All items moved successfully!")
	}

	// Moving the files inside tmp outside

	tmpDirectory := "/home/sormazabal/src/general-service/tmp"

	moveContentsOutOfTmp(tmpDirectory)

	// Setting update status to 0

	setUpdateStatus(0)

	// Setting the server

	err = restartServer()
	if err != nil {
		fmt.Println("❌ Restart failed:", err)
	}

}

// moveContentsToDir moves files and folders into a specified target directory
func moveContentsToDir(srcPaths []string, targetDir string) error {
	// Ensure the target directory exists
	err := os.MkdirAll(targetDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	for _, srcPath := range srcPaths {
		fileName := filepath.Base(srcPath)             // Extract the file/folder name
		destPath := filepath.Join(targetDir, fileName) // Construct the destination path

		// Check if destination already exists
		if _, err := os.Stat(destPath); err == nil {
			fmt.Printf("⚠️ Skipping %s → %s (already exists)\n", srcPath, destPath)
			continue
		}

		// Move the file or directory
		err := os.Rename(srcPath, destPath)
		if err != nil {
			return fmt.Errorf("failed to move %s → %s: %w", srcPath, destPath, err)
		}
		fmt.Printf("✅ Moved: %s → %s\n", srcPath, destPath)
	}

	return nil
}

// moveContentsOutOfTmp moves all files and folders from "tmp/" to its parent directory
func moveContentsOutOfTmp(tmpDir string) error {
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to read tmp directory: %w", err)
	}

	parentDir := filepath.Dir(tmpDir) // Get the parent directory

	for _, file := range files {
		srcPath := filepath.Join(tmpDir, file.Name())     // Full path inside tmp
		destPath := filepath.Join(parentDir, file.Name()) // Move outside tmp

		// Check if destination already exists
		if _, err := os.Stat(destPath); err == nil {
			fmt.Printf("⚠️ Skipping %s → %s (already exists)\n", srcPath, destPath)
			continue
		}

		// Move the file or directory
		err := os.Rename(srcPath, destPath)
		if err != nil {
			return fmt.Errorf("failed to move %s → %s: %w", srcPath, destPath, err)
		}
		fmt.Printf("✅ Moved: %s → %s\n", srcPath, destPath)
	}

	// Remove tmp directory if empty
	err = os.Remove(tmpDir)
	if err == nil {
		fmt.Println("🗑️ tmp directory removed after moving all contents!")
	} else {
		fmt.Println("⚠️ tmp directory not removed (might not be empty)")
	}

	return nil
}

// restartServer executes "./general-service serve --config=config/general-service.yml"
func restartServer() error {
	// Define the command and its arguments
	cmd := exec.Command("./general-service", "serve", "--config=config/general-service.yml")

	// Attach the output to the console
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start the new process
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	fmt.Println("✅ New server instance started. Exiting old process...")
	time.Sleep(1 * time.Second) // Allow new process to start before exiting

	// Exit the old process
	os.Exit(0)

	return nil
}

// Unzipping a .zip

func Unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Close(); err != nil {
			panic(err)
		}
	}()

	os.MkdirAll(dest, 0755)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
		} else {
			os.MkdirAll(filepath.Dir(path), f.Mode())
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range r.File {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}
	return nil
}

// Function to update update_status.json
func setUpdateStatus(value int) error {
	// Create struct with new value
	updateStatus := UpdateStatus{UpdateAvailable: value}

	// Convert struct to JSON
	file, err := json.MarshalIndent(updateStatus, "", "  ")
	if err != nil {
		return err
	}

	// Write JSON to file
	err = os.WriteFile(jsonFilePath, file, 0644)
	if err != nil {
		return err
	}

	return nil
}
