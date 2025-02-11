package updater

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2/google"

	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

// The following config is used to fetch a target from Jussi's GitHub repository example
const (
	metadataURL          = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/metadata"
	targetsURL           = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/targets"
	verbosity            = 4
	generateRandomFolder = false
	TimeForUpdaters      = 60 // In seconds
)

var (
	// Services that the client will require
	services = []string{"tunnel-integration"}
	// Defining desired layout fot the date
	layout                = "2006.01.02.15.04.05"
	serviceAccountKeyPath = "/home/sormazabal/artifact-downloader-key.json"
	SALTOFilePath         = "/mnt/c/SALTO/%s"
)

type indexInfo struct {
	Bytes  string `json:"bytes"`
	Path   string `json:"path"`
	Hashes struct {
		Sha256 string `json:"sha256"`
	} `json:"hashes"`
	Version     string `json:"version"`
	ReleaseDate string `json:"release-date"`
}

// Setting Services
func SettingServices(log metadata.Logger) (string, []string) {
	currentVersions := make([]string, len(services)) // Preallocate slice with correct size

	// Initialize environment - temporary folders, etc.
	metadataDir, err := InitEnvironment()
	if err != nil {
		log.Error(err, "Failed to initialize environment")
	}

	// Initialize client with Trust-On-First-Use
	err = InitTrustOnFirstUse(metadataDir)
	if err != nil {
		log.Error(err, "Trust-On-First-Use failed")
	}

	// Downloading the services in different go routines for the first time
	var wg sync.WaitGroup
	for i, service := range services {
		wg.Add(1)

		// Pass index as a parameter to avoid closure issues
		go func(index int, service string) {
			defer wg.Done()
			currentVersions[index] = processInitService(metadataDir, service) // Use = instead of :=
		}(i, service) // Pass i explicitly
	}

	wg.Wait() // Ensure all goroutines finish before returning

	fmt.Printf("\nInitialization completed successfully\n")

	return metadataDir, currentVersions
}

// Main Updater Process

func SettingUpdater(log metadata.Logger, metadataDir string, currentVersions []string) {
	// download the desired services
	for index, service := range services {

		targetIndexFile, foundDesiredTargetIndexLocally, err := DownloadTargetIndex(metadataDir, service)

		if err != nil {
			log.Error(err, "Download index file failed")
		}

		if foundDesiredTargetIndexLocally == 0 {

			// Verifying that the index.json's version is latest than the one that is currently running

			// Map to hold the top-level JSON keys
			var data map[string]indexInfo

			// Parse JSON into the map
			err = json.Unmarshal([]byte(targetIndexFile), &data)
			if err != nil {
				fmt.Printf("\U0001F534Error parsing JSON: %v\U0001F534", err)
			}
			// Latest version considering the index.json downloaded by TUF

			indexVersion := data[service].ReleaseDate

			newProductVersion := NewVersion(currentVersions[index], indexVersion, layout, service)

			if newProductVersion == 1 {
				fmt.Printf("There is a new product of  %s \n", service)

				// Getting user answer

				userAnswer := gettingUserAnswer()

				if userAnswer == 1 {

					// Service account key file
					servicePath := data[service].Path

					fmt.Printf("Downloading binary from: %s\n", servicePath)

					// Download the artifact without specifying the file type
					err = downloadArtifact(serviceAccountKeyPath, servicePath, service, metadataDir)
					if err != nil {
						fmt.Printf("\U0001F534Failed to download binary: %v\U0001F534\n", err)
						os.Exit(1)
					}

					downloadedFilePath := fmt.Sprintf("%s/%s", metadataDir, service)

					verifyingDownloadedFile(string(targetIndexFile), downloadedFilePath, service)

					currentVersions[index] = indexVersion

					// Printing expiration date
					//PrintExpirationDate(layout, currentVersions[index])

					fmt.Printf("\nThe current %s version is: %s \n", service, currentVersions[index])

					movingFileToPermanentLocation(service, downloadedFilePath)

				} else {

					fmt.Printf("\u23F0Remember that you have an update pending.\u23F0\n")
					// Telling the user the expiration date of the current version
					//PrintExpirationDate(layout, currentVersions[index])

				}

			} else {
				fmt.Printf("There is no new product\n")
			}

		} else {
			fmt.Printf("\nThe local index file is the most updated one\n")
		}
	}

	time.Sleep(time.Second * TimeForUpdaters)

}

// InitEnvironment prepares the local environment - temporary folders, etc.
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
	os.Mkdir(filepath.Join(tmpDir, "download"), 0750)
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

// DownloadTargetIndex downloads the target file using Updater. The Updater refreshes the top-level metadata,
// get the target information, verifies if the target is already cached, and in case it
// is not cached, downloads the target file.

func DownloadTargetIndex(localMetadataDir, service string) ([]byte, int, error) {

	serviceFilePath := fmt.Sprintf("%s/%s-index.json", service, service)

	fmt.Printf("The service path is: %s\n", serviceFilePath)

	rootBytes, err := os.ReadFile(filepath.Join(localMetadataDir, "root.json"))
	if err != nil {
		return nil, 0, err
	}

	// create updater configuration
	cfg, err := config.New(metadataURL, rootBytes) // default config
	if err != nil {
		return nil, 0, err
	}
	cfg.LocalMetadataDir = localMetadataDir
	cfg.LocalTargetsDir = filepath.Join(localMetadataDir, "download")
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

	ti, err := up.GetTargetInfo(serviceFilePath)
	if err != nil {
		return nil, 0, fmt.Errorf("getting info for target index \"%s\": %w", serviceFilePath, err)
	}

	path, tb, err := up.FindCachedTarget(ti, filepath.Join(localMetadataDir, serviceFilePath))
	if err != nil {
		return nil, 0, fmt.Errorf("getting target index cache: %w", err)
	}

	if path != "" {
		// Cached version found
		fmt.Println("\U0001F34C CACHE HIT")
		return tb, 1, nil
	}

	// Download of target is needed
	_, tb, err = up.DownloadTarget(ti, "", "")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download target index file %s - %w", service, err)
	}

	return tb, 0, nil
}

func gettingUserAnswer() int {

	var userAnswer int

	fmt.Printf("\n Do you want to download the new version?\n")

	fmt.Printf("\n Introduce your answer: \n")
	fmt.Println("------------------------------------------")
	fmt.Printf("\nFor YES => (1)")
	fmt.Printf("\nFor NO  => (2)\n")

	fmt.Scanf("%d", &userAnswer)

	return userAnswer

}

// downloadArtifact dynamically determines the file name and downloads the artifact
func downloadArtifact(keyFilePath, url, service, metadataDir string) error {
	// Authenticate using the service account key
	ctx := context.Background()
	creds, err := google.CredentialsFromJSON(ctx, readFile(keyFilePath), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return fmt.Errorf("failed to load service account credentials: %w", err)
	}

	// Create HTTP client with the token
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
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
	fileName := fmt.Sprintf("%s/%s", metadataDir, service)
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

func NewVersion(currentVersion, indexVersion, layout, service string) int {

	var newVersion int

	currentVersionParsed, err := time.Parse(layout, currentVersion)

	fmt.Printf("Current Version of %s : %s, \n", service, currentVersion)
	fmt.Printf("Index Version of %s: %s\n", service, indexVersion)

	if err != nil {
		fmt.Printf("\U0001F534Error parsing version of the current version running: %v\U0001F534\n", err)
	}

	indexVersionParsed, err := time.Parse(layout, indexVersion)

	if err != nil {
		fmt.Printf("\U0001F534Error parsing the version that the index.json indicates: %v\U0001F534\n", err)
	}

	if currentVersionParsed.Before(indexVersionParsed) {
		newVersion = 1
	} else if currentVersionParsed.After(indexVersionParsed) {
		newVersion = 0
	} else {
		newVersion = 0
	}
	return newVersion
}

// Printing the expiratin date of a version

func PrintExpirationDate(layout, currentVersion string) {
	// Parse the string into a time.Time object
	currentVersionParsed, err := time.Parse(layout, currentVersion)
	if err != nil {
		fmt.Printf("\U0001F534Error parsing the current version date: %v\U0001F534\n", err)
		return
	}

	fmt.Printf("Parsed version date: %v\n", currentVersionParsed)

	// Add 2 years to the parsed date
	expirationDateOfCurrentVersion := currentVersionParsed.AddDate(2, 0, 0)

	currentDate := time.Now()

	validTimeOfCurrentVersion := expirationDateOfCurrentVersion.Sub(currentDate)

	totalHours := int(validTimeOfCurrentVersion.Hours())
	totalDays := totalHours / 24
	years := totalDays / 365
	days := totalDays % 365
	hours := totalHours % 24
	minutes := int(validTimeOfCurrentVersion.Minutes()) % 60
	seconds := int(validTimeOfCurrentVersion.Seconds()) % 60

	fmt.Printf("\u23F0The current version will expire in %d years, %d days, %d hours, %d minutes, and %d seconds\u23F0\n",
		years, days, hours, minutes, seconds)
}

func verifyingDownloadedFile(indexPath, DonwloadedFilePath, service string) {

	// Hash of the index.json file
	var data map[string]indexInfo

	// Parse JSON into the map
	err := json.Unmarshal([]byte(indexPath), &data)
	if err != nil {
		fmt.Printf("\U0001F534Error parsing JSON: %v\U0001F534", err)
	}
	// Latest version considering the index.json downloaded by TUF

	indexHash := data[service].Hashes.Sha256

	// Computing the hash of the downloaded file

	// Compute the SHA256 hash
	downloadedFilehash, err := ComputeSHA256(DonwloadedFilePath)

	fmt.Printf("Downloaded file hash is: %s\n", downloadedFilehash)

	if err != nil {
		fmt.Printf("\U0001F534Error computing hash: %v\U0001F534\n", err)
		return
	}

	if indexHash == downloadedFilehash {
		fmt.Printf("\U0001F7E2The target file has been downloaded successfully!\U0001F7E2\n")
	} else {
		fmt.Printf("\U0001F534There has been an error while downloading the file, the hashes do not match\U0001F534\n")
		return
	}
}

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

func moveFile(srcPath, destPath string) error {
	// Open the source file
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Create the destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	// Copy the contents from source to destination
	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// Close the destination file before deleting the source
	destFile.Close()

	// Delete the source file
	err = os.Remove(srcPath)
	if err != nil {
		return fmt.Errorf("failed to delete source file: %w", err)
	}

	return nil
}

// Function for moving the file to the permanent location
func movingFileToPermanentLocation(service, downloadedFilePath string) {

	newFilePath := fmt.Sprintf(SALTOFilePath, service)

	// Moving from the temporary folder

	// Move the file
	err := moveFile(downloadedFilePath, newFilePath)
	if err != nil {
		fmt.Printf("Error moving file: %v\n", err)
	}

	fmt.Println("File moved successfully!")

}

func processInitService(metadataDir, service string) string {

	// Download the target index considering trusted targets role
	targetIndexFile, _, err := DownloadTargetIndex(metadataDir, service)

	if err != nil {
		fmt.Printf("Download index file failed")
	}

	// Map to hold the top-level JSON keys
	var data map[string]indexInfo

	// Parse JSON into the map
	err = json.Unmarshal([]byte(targetIndexFile), &data)
	if err != nil {
		fmt.Printf("Error parsing JSON: %v", err)
	}

	// Latest version considering the index.json downloaded by TUF

	servicePath := data[service].Path

	fmt.Printf("Downloading binary from: %s\n", servicePath)

	// Download the artifact without specifying the file type
	err = downloadArtifact(serviceAccountKeyPath, servicePath, service, metadataDir)
	if err != nil {
		fmt.Printf("Failed to download binary: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Target index file is: %s\n", targetIndexFile)
	downloadedFilePath := fmt.Sprintf("%s/%s", metadataDir, service)

	verifyingDownloadedFile(string(targetIndexFile), downloadedFilePath, service)

	currentVersions := data[service].ReleaseDate

	// Printing expiration date
	PrintExpirationDate(layout, currentVersions)

	fmt.Printf("\nThe current %s version is: %s \n", service, currentVersions)

	movingFileToPermanentLocation(service, downloadedFilePath)

	return currentVersions

}
