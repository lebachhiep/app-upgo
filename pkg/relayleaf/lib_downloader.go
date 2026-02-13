package relayleaf

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var downloadServers = []string{
	"https://release.prx.network",
	"https://github.com/lebachhiep/relay-leaf-library/releases/latest/download",
}

type checksumFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type checksumsResponse struct {
	Files []checksumFile `json:"files"`
}

func GetLibraryName() string {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "relay_leaf-windows-x64.dll"
		case "386":
			return "relay_leaf-windows-x86.dll"
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "librelay_leaf-linux-x64.so"
		case "arm64":
			return "librelay_leaf-linux-arm64.so"
		}
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return "librelay_leaf-darwin-arm64.dylib"
		case "amd64":
			return "librelay_leaf-darwin-amd64.dylib"
		}
	}
	return ""
}

func ComputeFileHash(filepath string) (string, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// LogFunc is called with status messages during EnsureLibrary.
var LogFunc func(msg string)

func logMsg(msg string) {
	if LogFunc != nil {
		LogFunc(msg)
	}
}

func EnsureLibrary(libraryPath string) bool {
	libName := GetLibraryName()
	if libName == "" {
		logMsg("Unsupported platform")
		return false
	}

	if libraryPath == "" {
		exePath, err := os.Executable()
		if err != nil {
			return false
		}
		libraryPath = filepath.Join(filepath.Dir(exePath), libName)
	}

	// Try extracting embedded library if file doesn't exist on disk yet
	if _, err := os.Stat(libraryPath); os.IsNotExist(err) {
		if ExtractEmbeddedLibrary(libName, libraryPath) {
			logMsg("Extracted embedded library")
		}
	}

	logMsg("Fetching remote checksum...")
	expectedHash := fetchExpectedHash(libName)

	hasExisting := false
	if _, err := os.Stat(libraryPath); err == nil {
		hasExisting = true
		if expectedHash != "" {
			localHash, err := ComputeFileHash(libraryPath)
			if err == nil && strings.EqualFold(localHash, expectedHash) {
				logMsg("Library is up to date")
				return true
			}
			logMsg("Hash mismatch, updating library...")
		} else {
			// Checksum server unreachable — use existing file
			logMsg("Library exists, checksum server unreachable")
			return true
		}
	} else {
		// File doesn't exist — try embedded extraction one more time
		if ExtractEmbeddedLibrary(libName, libraryPath) {
			logMsg("Extracted embedded library")
			return true
		}
		logMsg("Library not found, downloading...")
	}

	// Backup existing library before overwriting so we can restore on failure
	backupPath := libraryPath + ".bak"
	if hasExisting {
		os.Remove(backupPath)
		if err := os.Rename(libraryPath, backupPath); err != nil {
			logMsg("Warning: could not backup existing library")
		}
	}

	for i, server := range downloadServers {
		url := fmt.Sprintf("%s/%s", server, libName)
		logMsg(fmt.Sprintf("Downloading from server %d/%d...", i+1, len(downloadServers)))
		if downloadFile(url, libraryPath) {
			if expectedHash != "" {
				localHash, err := ComputeFileHash(libraryPath)
				if err == nil && strings.EqualFold(localHash, expectedHash) {
					logMsg("Download complete, hash verified")
					os.Remove(backupPath)
					return true
				}
				logMsg("Hash verification failed, trying next server...")
				os.Remove(libraryPath)
				continue
			}
			logMsg("Download complete")
			os.Remove(backupPath)
			return true
		}
		logMsg(fmt.Sprintf("Server %d failed", i+1))
	}

	// All servers failed — restore backup if available
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Rename(backupPath, libraryPath); err == nil {
			logMsg("Update failed, using existing library")
			return true
		}
	}

	if _, err := os.Stat(libraryPath); err == nil {
		return true
	}

	logMsg("All download servers failed")
	return false
}

func fetchExpectedHash(libName string) string {
	client := &http.Client{Timeout: 10 * time.Second}

	for _, server := range downloadServers {
		hash := func() string {
			url := fmt.Sprintf("%s/checksums.json", server)
			resp, err := client.Get(url)
			if err != nil {
				return ""
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return ""
			}

			var checksums checksumsResponse
			if err := json.NewDecoder(resp.Body).Decode(&checksums); err != nil {
				return ""
			}

			for _, f := range checksums.Files {
				if f.Name == libName {
					return f.SHA256
				}
			}
			return ""
		}()
		if hash != "" {
			return hash
		}
	}

	return ""
}

func downloadFile(url, dest string) bool {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return false
	}

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}

	f, err := os.Create(dest)
	if err != nil {
		return false
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(dest)
		return false
	}

	return true
}
