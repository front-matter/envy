package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMissingRequiredGitignoreEntries_AllPresent(t *testing.T) {
	dir := t.TempDir()
	content := ".env\ncompose.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore): %v", err)
	}

	missing, err := missingRequiredGitignoreEntries(dir, requiredGitignoreEntries)
	if err != nil {
		t.Fatalf("missingRequiredGitignoreEntries(): %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected no missing entries, got: %v", missing)
	}
}

func TestMissingRequiredGitignoreEntries_MissingFile(t *testing.T) {
	dir := t.TempDir()

	missing, err := missingRequiredGitignoreEntries(dir, requiredGitignoreEntries)
	if err != nil {
		t.Fatalf("missingRequiredGitignoreEntries(): %v", err)
	}
	if !reflect.DeepEqual(missing, requiredGitignoreEntries) {
		t.Fatalf("expected all required entries missing, got: %v", missing)
	}
}

func TestMissingRequiredGitignoreEntries_PatternsSupported(t *testing.T) {
	dir := t.TempDir()
	content := "# ignore local env files\n.env*\n**/compose.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore): %v", err)
	}

	missing, err := missingRequiredGitignoreEntries(dir, requiredGitignoreEntries)
	if err != nil {
		t.Fatalf("missingRequiredGitignoreEntries(): %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected no missing entries, got: %v", missing)
	}
}

func TestMissingRequiredGitignoreEntries_OneMissing(t *testing.T) {
	dir := t.TempDir()
	content := ".env\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore): %v", err)
	}

	missing, err := missingRequiredGitignoreEntries(dir, requiredGitignoreEntries)
	if err != nil {
		t.Fatalf("missingRequiredGitignoreEntries(): %v", err)
	}
	expected := []string{"compose.yaml"}
	if !reflect.DeepEqual(missing, expected) {
		t.Fatalf("expected %v, got %v", expected, missing)
	}
}
