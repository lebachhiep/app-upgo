package relayleaf

import (
	"os"
	"path/filepath"
)

// ExtractEmbeddedLibrary extracts the named library from the embedded FS
// to destPath. Returns true if extraction succeeded, false if the library
// is not embedded or extraction failed.
func ExtractEmbeddedLibrary(libName, destPath string) bool {
	data, err := embeddedLibs.ReadFile("libs/" + libName)
	if err != nil {
		return false
	}
	if len(data) == 0 {
		return false
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}

	if err := os.WriteFile(destPath, data, 0755); err != nil {
		os.Remove(destPath)
		return false
	}

	return true
}
