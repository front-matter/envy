package compose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultManifestFilename = "compose.yml"

var ManifestFilenames = []string{
	"compose.yml",
	"compose.yaml",
	"docker-compose.yml",
	"docker-compose.yaml",
}

// ErrManifestNotFound is returned when no compose manifest is found.
var ErrManifestNotFound = errors.New("compose manifest not found (tried compose.yml, compose.yaml, docker-compose.yml, docker-compose.yaml)")

// FindIn returns the compose manifest in dir, without looking at its parents.
// The file names are tried in the order of ManifestFilenames.
func FindIn(dir string) (string, error) {
	for _, name := range ManifestFilenames {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return "", fmt.Errorf("%w in %s — pass a path or --file", ErrManifestNotFound, dir)
}

// Find walks up from the current directory looking for a compose manifest.
func Find() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		for _, name := range ManifestFilenames {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return "", fmt.Errorf("%w — run from your instance root or pass --file", ErrManifestNotFound)
}
