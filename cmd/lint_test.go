package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSortXSetBlocksInComposeYAML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yaml")

	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-mail: &mail",
		"  A: \"1\"",
		"x-set-base: &base",
		"  B: \"1\"",
		"x-set-authentication: &authentication",
		"  C: \"1\"",
		"services:",
		"  web:",
		"    image: ghcr.io/example/web:v1",
		"    environment:",
		"      <<: [*base, *mail, *authentication]",
	}, "\n")

	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatalf("write compose.yaml: %v", err)
	}

	fixed, err := sortXSetBlocksInComposeYAML(path)
	if err != nil {
		t.Fatalf("sortXSetBlocksInComposeYAML() error = %v", err)
	}
	if !fixed {
		t.Fatalf("expected file to be fixed")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	output := string(data)

	basePos := strings.Index(output, "x-set-base:")
	authPos := strings.Index(output, "x-set-authentication:")
	mailPos := strings.Index(output, "x-set-mail:")
	if basePos == -1 || authPos == -1 || mailPos == -1 {
		t.Fatalf("expected all x-set blocks in output, got:\n%s", output)
	}
	if !(basePos < authPos && authPos < mailPos) {
		t.Fatalf("expected x-set-base first then alphabetical order, got:\n%s", output)
	}
}

func TestSortXSetBlocksInComposeYAMLNoChange(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yaml")

	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-base: &base",
		"  B: \"1\"",
		"x-set-authentication: &authentication",
		"  C: \"1\"",
		"x-set-mail: &mail",
		"  A: \"1\"",
		"services:",
		"  web:",
		"    image: ghcr.io/example/web:v1",
	}, "\n")

	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatalf("write compose.yaml: %v", err)
	}

	fixed, err := sortXSetBlocksInComposeYAML(path)
	if err != nil {
		t.Fatalf("sortXSetBlocksInComposeYAML() error = %v", err)
	}
	if fixed {
		t.Fatalf("expected no changes for already sorted file")
	}
}
