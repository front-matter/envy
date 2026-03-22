package compose

import (
	"errors"
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
	return "", errors.New(
		"compose manifest not found (tried compose.yml, compose.yaml, docker-compose.yml, docker-compose.yaml) — run from your instance root or pass --file",
	)
}
