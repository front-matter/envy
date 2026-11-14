package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/front-matter/envy/compose"
)

func TestValidateComposeFileCallsComposeConfigRunner(t *testing.T) {
	path := "./compose.yaml"

	originalRunner := composeConfigRunner
	t.Cleanup(func() {
		composeConfigRunner = originalRunner
	})

	called := false
	composeConfigRunner = func(gotPath string) (string, error) {
		called = true
		if gotPath == "" {
			t.Fatalf("runner got empty path")
		}
		return "", nil
	}

	if err := validateComposeFile(path); err != nil {
		t.Fatalf("validateComposeFile() error = %v", err)
	}
	if !called {
		t.Fatalf("expected composeConfigRunner to be called")
	}
}

func TestValidateComposeFileIncludesComposeConfigOutputOnError(t *testing.T) {
	originalRunner := composeConfigRunner
	t.Cleanup(func() {
		composeConfigRunner = originalRunner
	})

	composeConfigRunner = func(_ string) (string, error) {
		return "services.web.image is required", errors.New("exit status 1")
	}

	err := validateComposeFile("./compose.yaml")
	if err == nil {
		t.Fatalf("expected validateComposeFile to fail")
	}
	if got := err.Error(); !containsAll(got, "invalid compose file", "services.web.image is required") {
		t.Fatalf("expected detailed compose output in error, got: %s", got)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func TestValidateUsesCurrentDirectoryOnly(t *testing.T) {
	parent := t.TempDir() // no .gitignore anywhere
	if err := os.WriteFile(filepath.Join(parent, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("writing parent manifest: %v", err)
	}
	dir := filepath.Join(parent, "sub")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd) // best effort: restore the working directory for other tests
	})

	var validated string
	origRunner := composeConfigRunner
	composeConfigRunner = func(path string) (string, error) {
		validated = path
		return "", nil
	}
	t.Cleanup(func() { composeConfigRunner = origRunner })

	rootCmd.SetArgs([]string{"validate"})
	if err := rootCmd.Execute(); !errors.Is(err, compose.ErrManifestNotFound) {
		t.Fatalf("envy validate without a manifest here: error = %v, want ErrManifestNotFound", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}
	rootCmd.SetArgs([]string{"validate"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("envy validate without .gitignore: %v", err)
	}
	// Compare base names: on macOS the temp dir is reached through a symlink.
	if filepath.Base(validated) != "compose.yml" || filepath.Base(filepath.Dir(validated)) != "sub" {
		t.Fatalf("validated %q, want compose.yml in %q", validated, dir)
	}
}
