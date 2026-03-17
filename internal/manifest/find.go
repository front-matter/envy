package manifest

import (
	"errors"
	"os"
	"path/filepath"
)

// Find walks up from the current directory looking for env.yaml.
func Find() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(dir, "env.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return "", errors.New(
		"env.yaml not found — run from your instance root or pass --manifest",
	)
}
