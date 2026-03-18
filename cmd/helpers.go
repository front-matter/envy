package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/front-matter/envy/manifest"
)

// resolveManifest returns the manifest path from the flag or auto-detection.
func resolveManifest(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	return manifest.Find()
}

// resolveCommandFilePath resolves a file-or-folder output path.
// File paths create parent directories as needed.
// Folder paths are created and defaultFilename is appended.
func resolveCommandFilePath(path string, defaultFilename string) (string, error) {
	if path == "" {
		return "", nil
	}

	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return filepath.Join(path, defaultFilename), nil
		}
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("creating directory %s: %w", dir, err)
			}
		}
		return path, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}

	if strings.HasSuffix(path, string(os.PathSeparator)) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return "", fmt.Errorf("creating directory %s: %w", path, err)
		}
		return filepath.Join(path, defaultFilename), nil
	}

	base := filepath.Base(path)
	if filepath.Ext(base) != "" || strings.HasPrefix(base, ".") {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("creating directory %s: %w", dir, err)
			}
		}
		return path, nil
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", path, err)
	}

	return filepath.Join(path, defaultFilename), nil
}

func resolveEnvInputPath(args []string) (string, error) {
	if len(args) == 0 {
		paths, err := findImportFiles(".")
		if err != nil {
			return "", err
		}
		for _, path := range paths {
			lower := strings.ToLower(filepath.Base(path))
			if lower == ".env" || lower == ".env.example" {
				return path, nil
			}
		}
		return "", fmt.Errorf("no env file found (tried auto-detection of .env and .env.example)")
	}

	inputPath := args[0]
	info, err := os.Stat(inputPath)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", inputPath, err)
	}

	if !info.IsDir() {
		return inputPath, nil
	}

	paths, err := findImportFiles(inputPath)
	if err != nil {
		return "", err
	}
	for _, path := range paths {
		lower := strings.ToLower(filepath.Base(path))
		if lower == ".env" || lower == ".env.example" {
			return path, nil
		}
	}

	return "", fmt.Errorf("no env file found in %s (tried .env and .env.example)", inputPath)
}
