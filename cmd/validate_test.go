package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateComposeFileCallsComposeConfigRunner(t *testing.T) {
	path := "./env.yaml"

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

	err := validateComposeFile("./env.yaml")
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
