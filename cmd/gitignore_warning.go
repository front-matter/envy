package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var requiredGitignoreEntries = []string{".env", "compose.yaml"}

func warnMissingGitignoreEntries() {
	missing, err := missingRequiredGitignoreEntries(".", requiredGitignoreEntries)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not verify .gitignore entries: %v\n", err)
		return
	}

	if len(missing) == 0 {
		return
	}

	fmt.Fprintf(
		os.Stderr,
		"Warning: %s is missing recommended entries: %s\n",
		filepath.Join(".", ".gitignore"),
		strings.Join(missing, ", "),
	)
}

func missingRequiredGitignoreEntries(dir string, required []string) ([]string, error) {
	path := filepath.Join(dir, ".gitignore")
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return append([]string(nil), required...), nil
		}
		return nil, err
	}

	lines := strings.Split(string(content), "\n")
	present := make(map[string]bool, len(required))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		for _, entry := range required {
			if matchesGitignoreEntry(line, entry) {
				present[entry] = true
			}
		}
	}

	missing := make([]string, 0, len(required))
	for _, entry := range required {
		if !present[entry] {
			missing = append(missing, entry)
		}
	}

	return missing, nil
}

func matchesGitignoreEntry(line, target string) bool {
	if line == target || line == "/"+target || line == "**/"+target {
		return true
	}

	matched, err := filepath.Match(line, target)
	if err == nil && matched {
		return true
	}

	if strings.HasPrefix(line, "**/") {
		matched, err = filepath.Match(strings.TrimPrefix(line, "**/"), target)
		if err == nil && matched {
			return true
		}
	}

	return false
}
