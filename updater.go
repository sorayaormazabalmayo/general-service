package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	stdlog "log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/stdr"
	"golang.org/x/oauth2/google"

	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

// The following config is used to fetch a target from Jussi's GitHub repository example
const (
	metadataURL          = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/metadata"
	targetsURL           = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/targets"
	verbosity            = 0
	generateRandomFolder = false
)

var (
	serviceAccountKeyPath = "/home/sormazabal/artifact-downloader-key.json"
	jsonFilePath          = "/home/sormazabal/src/SALTO2/update_status.json"
	service               = "general-service"
	targetIndexFile       = "/home/sormazabal/src/SALTO2/data/general-service/general-service-index.json"
	newBinaryPath         = "/home/sormazabal/src/SALTO2/tmp/general-service.zip"
	destinationPath       = "/home/sormazabal/src/SALTO2/general-service.zip"
	SALTOLocation         = "/home/sormazabal/src/SALTO2"
)

// struct to store update status
type UpdateStatus struct {
	UpdateAvailable int `json:"update_available"`
	UpdateRequested int `json:"update_requested"`
}

// indexInfo is the structure in which the information from the general-service.json is stored.
type indexInfo struct {
	Bytes  string `json:"bytes"`
	Path   string `json:"path"`
	Hashes struct {
		Sha256 string `json:"sha256"`
	} `json:"hashes"`
	Version     string `json:"version"`
	ReleaseDate string `json:"release-date"`
}

// Main program
func main() {

	previousVersion := ""
	currentVersion := ""

	// these are the first steps for performing the initial configuration

	// set logger to stdout with info level
	metadata.SetLogger(stdr.New(stdlog.New(os.Stdout, "Nebula_TUF_Client:", stdlog.LstdFlags)))

	// the verbosity sets the level of detail of the logger
	stdr.SetVerbosity(verbosity)
	log := metadata.GetLogger()

	// initialize environment - temporary folders, etc.
	metadataDir, err := InitEnvironment()
	if err != nil {
		log.Error(err, "Failed to initialize environment")
	}

	// initialize client with Trust-On-First-Use
	err = InitTrustOnFirstUse(metadataDir)
	if err != nil {
		log.Error(err, "Trust-On-First-Use failed")
	}

	var wg sync.WaitGroup
	wg.Add(1)

	currentVersion, err = readCurrentVersion()

	if err != nil {
		fmt.Printf("There has been an error wile reading the current version\n")
	}

	// Go routine 1 for setting the TUF updater
	go func() {
		defer wg.Done()

		// the updater needs to be looking for new updates every x time
		for {

			// downloading general-service-index.json
			_, foundDesiredTargetIndexLocally, err := DownloadTargetIndex(metadataDir, service)

			if err != nil {
				log.Error(err, "Download index file failed")
			}

			// if there is a new one, this will mean that is initializing for the first time or that there is a new update
			if foundDesiredTargetIndexLocally == 0 {
				err := setUpdateStatus(1)
				if err != nil {
					fmt.Println("❌ Error updating update_status.json:", err)
				} else {
					fmt.Println("✅ Successfully set update_status.json to update_available: 1")
				}

			} else {
				fmt.Printf("\nThe local index file is the most updated one\n")
			}

			time.Sleep(time.Second * 60)

		}
	}()
	//

	// Go routine 2 that is alsways looking if the user has requested the update
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {

			// every x time it will be reading if the user has requested a new update
			updateRequested, err := ReadUpdateRequested(jsonFilePath)

			if err != nil {
				fmt.Printf("There has been an error while reading the Update Requested Value: %f. \n", err)
			}

			// if the user has pushed the botton, the new server should be executed.
			if updateRequested == 1 {

				// get working directory
				cwd, err := os.Getwd()
				if err != nil {
					fmt.Printf("Failed to get the working directory \n")
				}
				tmpDir := filepath.Join(cwd, "tmp")
				// create a temporary folder for storing the demo artifacts
				os.Mkdir(tmpDir, 0750)

				var data map[string]indexInfo

				fmt.Printf("The index file is located in: %s \n", targetIndexFile)

				// read the actual JSON file content
				fileContent, err := os.ReadFile(targetIndexFile)
				if err != nil {
					fmt.Printf("Fail to read the index file \n")
				}

				// parse JSON into the map
				err = json.Unmarshal(fileContent, &data)
				if err != nil {
					fmt.Printf("\U0001F534Error parsing JSON: %v\U0001F534", err)
				}

				// getting service path
				servicePath := data[service].Path

				// download the artifact without specifying the file type
				err = downloadArtifact(serviceAccountKeyPath, servicePath, newBinaryPath)
				if err != nil {
					fmt.Printf("\U0001F534Failed to download binary: %v\U0001F534\n", err)
					os.Exit(1)
				}

				// make sure the new binary is executable
				err = os.Chmod(newBinaryPath, 0755)
				if err != nil {
					fmt.Printf("Failed to set executable permissions \n")
				}

				// verifying that the downloaded file is integrate and authentic
				err = verifyingDownloadedFile(targetIndexFile, newBinaryPath)

				if err == nil {
					// Replace old binary
					err = os.Rename(newBinaryPath, destinationPath)
					if err != nil {
						fmt.Printf("Failed to rename the binary \n")
					}
				}

				serviceVersion := data[service].Version

				// unziping and setting the update status to 0
				unzipAndSetStatus(serviceVersion)

				// restarting the server
				err = restartServer(serviceVersion)
				if err != nil {
					fmt.Printf("Failed to restart the server: %f", err)
				}

				// If the server has been properly started

				// Delete the previous version's folder

				previousVersionPath := fmt.Sprintf("%s/%s", SALTOLocation, previousVersion)

				err = os.RemoveAll(previousVersionPath)

				if err != nil {
					fmt.Printf("Error deleting the previous version's folder\n")
				}

				// The previus version is what has been stored in current version
				previousVersion = currentVersion

				currentVersion, err = readCurrentVersion()

				if err != nil {
					fmt.Printf("Error reading the current version")
				}

			}
			time.Sleep(time.Second * 5)
		}
	}()
	//
	wg.Wait()
}

// InitEnvironment prepares the local environment for TUF- temporary folders, etc.
func InitEnvironment() (string, error) {
	var tmpDir string
	// get working directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}
	if !generateRandomFolder {
		tmpDir = filepath.Join(cwd, "tmp")
		// create a temporary folder for storing the demo artifacts
		os.Mkdir(tmpDir, 0750)
	} else {
		// create a temporary folder for storing the demo artifacts
		tmpDir, err = os.MkdirTemp(cwd, "tmp")
		if err != nil {
			return "", fmt.Errorf("failed to create a temporary folder: %w", err)
		}
	}

	// create a destination folder for storing the downloaded target
	os.Mkdir(filepath.Join(cwd, "data"), 0750)
	return tmpDir, nil
}

// InitTrustOnFirstUse initialize local trusted metadata (Trust-On-First-Use)
func InitTrustOnFirstUse(metadataDir string) error {
	// check if there's already a local root.json available for bootstrapping trust
	_, err := os.Stat(filepath.Join(metadataDir, "root.json"))
	if err == nil {
		return nil
	}

	// download the initial root metadata so we can bootstrap Trust-On-First-Use
	rootURL, err := url.JoinPath(metadataURL, "1.root.json")
	if err != nil {
		return fmt.Errorf("failed to create URL path for 1.root.json: %w", err)
	}

	req, err := http.NewRequest("GET", rootURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	client := http.DefaultClient

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to executed the http request: %w", err)
	}

	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read the http request body: %w", err)
	}

	// write the downloaded root metadata to file
	err = os.WriteFile(filepath.Join(metadataDir, "root.json"), data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write root.json metadata: %w", err)
	}
	return nil
}

// Reading the version of the current running server. For that, the general_service_index.json
// version will be downloaded.

func readCurrentVersion() (string, error) {

	var data map[string]indexInfo

	// Read the actual JSON file content
	fileContent, err := os.ReadFile(targetIndexFile)
	if err != nil {
		return "", fmt.Errorf("failed to read index file: %w", err)
	}

	// Parse JSON into the map
	err = json.Unmarshal(fileContent, &data)
	if err != nil {
		return "", fmt.Errorf("error parsin the JSON: %w", err)
	}

	currentVersion := data[service].Version

	return currentVersion, nil
}

// DownloadTargetIndex downloads the target file using Updater. The Updater refreshes the top-level metadata,
// get the target information, verifies if the target is already cached, and in case it
// is not cached, downloads the target file.
func DownloadTargetIndex(localMetadataDir, service string) ([]byte, int, error) {

	serviceFilePath := filepath.Join(service, fmt.Sprintf("%s-index.json", service))

	fmt.Printf("DEBUG: Constructed serviceFilePath: %s\n", serviceFilePath)
	decodedServiceFilePath, _ := url.QueryUnescape(serviceFilePath)
	fmt.Printf("DEBUG: Decoded serviceFilePath: %s\n", decodedServiceFilePath)

	rootBytes, err := os.ReadFile(filepath.Join(localMetadataDir, "root.json"))
	if err != nil {
		return nil, 0, err
	}

	// create updater configuration
	cfg, err := config.New(metadataURL, rootBytes) // default config
	if err != nil {
		return nil, 0, err
	}

	// get working directory
	cwd, err := os.Getwd()

	if err != nil {
		fmt.Printf("Error getting the current directory \n")
	}

	cfg.LocalMetadataDir = localMetadataDir
	cfg.LocalTargetsDir = filepath.Join(cwd, "data")
	cfg.RemoteTargetsURL = targetsURL
	cfg.PrefixTargetsWithHash = true

	// create a new Updater instance
	up, err := updater.New(cfg)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create Updater instance: %w", err)
	}

	// try to build the top-level metadata
	err = up.Refresh()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to refresh trusted metadata: %w", err)
	}

	fmt.Printf("DEBUG: serviceFilePath before GetTargetInfo: %s\n", serviceFilePath)

	// Decode serviceFilePath before calling GetTargetInfo
	decodedServiceFilePath, _ = url.QueryUnescape(serviceFilePath)
	fmt.Printf("DEBUG: Decoded serviceFilePath: %s\n", decodedServiceFilePath)

	// Get metadata info
	ti, err := up.GetTargetInfo(decodedServiceFilePath)
	if err != nil {
		return nil, 0, fmt.Errorf("getting info for target index \"%s\": %w", serviceFilePath, err)
	}

	os.Mkdir(filepath.Join(cwd, "data", service), 0750)

	targetFilePath := filepath.Join(cwd, "data", service, fmt.Sprintf("%s-index.json", service))
	os.MkdirAll(filepath.Dir(targetFilePath), 0750) // Ensure the directory exists

	path, tb, err := up.FindCachedTarget(ti, targetFilePath)

	fmt.Printf("DEBUG: Cached target file path: %s\n", path)

	if err != nil {
		return nil, 0, fmt.Errorf("failed to find if there is a cachet target: %w", err)
	}

	if path != "" {
		// Cached version found
		fmt.Println("\U0001F34C CACHE HIT")
		return tb, 1, nil
	}

	// Print the path before downloading
	fmt.Printf("DEBUG: targetFilePath before DownloadTarget: %s\n", targetFilePath)

	// Ensure it is unescaped
	decodedTargetFilePath, _ := url.QueryUnescape(targetFilePath)
	fmt.Printf("DEBUG: Decoded targetFilePath: %s\n", decodedTargetFilePath)

	// Now download
	targetfilePath, tb, err := up.DownloadTarget(ti, decodedTargetFilePath, "")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download target index file %s - %w", service, err)
	}

	fmt.Printf(" 🎯📄The target File Path is: %s 🎯📄", targetfilePath)

	return tb, 0, nil
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

// ReadUpdateRequested extracts the "update_requested" value from a JSON file
func ReadUpdateRequested(jsonFilePath string) (int, error) {
	// Read the JSON file content
	fileContent, err := os.ReadFile(jsonFilePath)
	if err != nil {
		return 0, fmt.Errorf("failed to read JSON file: %v", err)
	}

	// Unmarshal JSON into struct
	var status UpdateStatus
	err = json.Unmarshal(fileContent, &status)
	if err != nil {
		return 0, fmt.Errorf("error parsing JSON: %v", err)
	}

	return status.UpdateRequested, nil
}

// Downloading the artifact indicated in general-service.json
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

// readFile reads the content of the service account key JSON file.
func readFile(path string) []byte {
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("\U0001F534Error reading file %s: %v\U0001F534\n", path, err)
		os.Exit(1)
	}
	return content
}

// Verifying a file.
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

// Computing the SHA256 of a file.
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

// Unzipping the downloaded target and setting the update status to 0.
func unzipAndSetStatus(serviceVersion string) {

	destinationPathUnzip := ""
	destinationPathUnzip = fmt.Sprintf("%s/%s", SALTOLocation, serviceVersion)

	if err := Unzip(destinationPath, destinationPathUnzip); err != nil {
		fmt.Printf("❌ Error unzipping new binary: %v\n", err)
	} else {
		fmt.Println("✅ Successfully unzipped the new binary.")
	}

	// Removing what has been unzipped

	os.Remove(destinationPath)

	// Setting update status to 0

	setUpdateStatus(0)

}

// Unzipping a .zip and relocating it.
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

// RestartServer executes "./general-service serve --config=config/general-service.yml".
func restartServer(serviceVersion string) error {
	// Exec path
	execPath := fmt.Sprintf("%s/%s/general-service", SALTOLocation, serviceVersion)
	// Define the command and its arguments
	cmd := exec.Command(execPath, "serve", "--config=config/general-service.yml")

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

	return nil
}
