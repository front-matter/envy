package cmd

import "github.com/front-matter/envy/internal/manifest"

// resolveManifest returns the manifest path from the flag or auto-detection.
func resolveManifest(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	return manifest.Find()
}
