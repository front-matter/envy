package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var requiredGitignoreEntries = []string{".env"}

func ensureRequiredGitignoreEntries() error {
	missing, err := missingRequiredGitignoreEntries(".", requiredGitignoreEntries)
	if err != nil {
		return fmt.Errorf("could not verify .gitignore entries: %w", err)
	}

	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%s is missing required entries: %s",
		filepath.Join(".", ".gitignore"),
		strings.Join(missing, ", "),
	)
}

// missingRequiredGitignoreEntries returns the required entries that no
// .gitignore covers for files in dir. Like git, it reads the .gitignore files
// in dir and in its parents up to the repository root (the first directory
// with a .git entry), so a subdirectory of a repository whose root .gitignore
// lists .env passes. Outside a repository only dir's own .gitignore counts.
// Entries anchored with a leading slash only count in dir itself, because in
// a parent they match the parent's file, not dir's.
func missingRequiredGitignoreEntries(dir string, required []string) ([]string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dir, err)
	}

	dirs := []string{abs}
	if root, ok := repositoryRoot(abs); ok {
		for d := abs; d != root; {
			d = filepath.Dir(d)
			dirs = append(dirs, d)
		}
	}

	present := make(map[string]bool, len(required))
	for i, d := range dirs {
		if err := addGitignoreEntries(filepath.Join(d, ".gitignore"), required, i == 0, present); err != nil {
			return nil, err
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

// repositoryRoot returns the nearest directory at or above dir that contains
// a .git entry (directory, or file for worktrees and submodules).
func repositoryRoot(dir string) (string, bool) {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

// addGitignoreEntries marks the required entries that the .gitignore at path
// covers. A missing file covers nothing. anchoredOK reports whether entries
// with a leading slash apply, i.e. whether path is in the checked directory.
func addGitignoreEntries(path string, required []string, anchoredOK bool, present map[string]bool) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}

	for _, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "/") && !anchoredOK {
			continue
		}

		for _, entry := range required {
			if matchesGitignoreEntry(line, entry) {
				present[entry] = true
			}
		}
	}
	return nil
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
