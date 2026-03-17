// Package envfile reads and writes .env files.
package envfile

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Env is a parsed .env file: ordered keys and a key→value map.
type Env struct {
	Keys   []string
	Values map[string]string
}

// Load parses a .env file, skipping blank lines and comments.
func Load(path string) (*Env, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening env file %s: %w", path, err)
	}
	defer f.Close()

	e := &Env{Values: make(map[string]string)}
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.Trim(strings.TrimSpace(v), `"'`)
		e.Keys = append(e.Keys, key)
		e.Values[key] = val
	}

	return e, scanner.Err()
}

// Write writes content to a file with 0644 permissions.
func Write(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
