package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCommandFilePathFolder(t *testing.T) {
	tmp := t.TempDir()
	folder := filepath.Join(tmp, "docs")

	got, err := resolveCommandFilePath(folder, "ENV.md")
	if err != nil {
		t.Fatalf("resolveCommandFilePath(folder): %v", err)
	}

	want := filepath.Join(folder, "ENV.md")
	if got != want {
		t.Fatalf("resolveCommandFilePath(folder) = %q, want %q", got, want)
	}

	info, err := os.Stat(folder)
	if err != nil {
		t.Fatalf("expected output folder to be created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", folder)
	}
}

func TestResolveCommandFilePathDotFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, ".env.example")

	got, err := resolveCommandFilePath(path, ".env")
	if err != nil {
		t.Fatalf("resolveCommandFilePath(dotfile): %v", err)
	}

	if got != path {
		t.Fatalf("resolveCommandFilePath(dotfile) = %q, want %q", got, path)
	}
}
