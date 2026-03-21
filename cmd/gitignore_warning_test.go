package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMissingRequiredGitignoreEntries_AllPresent(t *testing.T) {
	dir := t.TempDir()
	content := ".env\n"
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
	content := "# ignore local env files\n.env*\n"
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
	content := "# no required entries\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore): %v", err)
	}

	missing, err := missingRequiredGitignoreEntries(dir, requiredGitignoreEntries)
	if err != nil {
		t.Fatalf("missingRequiredGitignoreEntries(): %v", err)
	}
	expected := []string{".env"}
	if !reflect.DeepEqual(missing, expected) {
		t.Fatalf("expected %v, got %v", expected, missing)
	}
}

func TestEnsureRequiredGitignoreEntries_AllPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".env\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore): %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd(): %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}

	if err := ensureRequiredGitignoreEntries(); err != nil {
		t.Fatalf("ensureRequiredGitignoreEntries() unexpected error: %v", err)
	}
}

func TestEnsureRequiredGitignoreEntries_MissingEntryReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("# intentionally empty\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore): %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd(): %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}

	err = ensureRequiredGitignoreEntries()
	if err == nil {
		t.Fatalf("expected error when .gitignore misses required entry")
	}
	if !strings.Contains(err.Error(), ".gitignore is missing required entries: .env") {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}
