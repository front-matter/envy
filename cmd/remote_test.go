package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCompose = `# Example stack
services:
  web:
    image: nginx:1.27
`

func setImportOutput(t *testing.T) string {
	t.Helper()
	outputPath := filepath.Join(t.TempDir(), "compose.yml")
	old := importFilePath
	importFilePath = outputPath
	t.Cleanup(func() { importFilePath = old })
	return outputPath
}

func TestIsURL(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"https://example.org/.env", true},
		{"http://example.org/.env", true},
		{".env", false},
		{"./config", false},
		{"ftp://example.org/.env", false},
		{"https://", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := isURL(tt.in); got != tt.want {
				t.Fatalf("isURL(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestFetchFirstFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compose.yaml" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(testCompose))
	}))
	t.Cleanup(srv.Close)

	gotURL, body, err := fetchFirst([]string{srv.URL + "/compose.yml", srv.URL + "/compose.yaml"})
	if err != nil {
		t.Fatalf("fetchFirst: %v", err)
	}
	if gotURL != srv.URL+"/compose.yaml" || string(body) != testCompose {
		t.Fatalf("fetchFirst = %q, %q", gotURL, body)
	}

	_, _, err = fetchFirst([]string{srv.URL + "/missing.yml"})
	if !errors.Is(err, errRemoteNotFound) {
		t.Fatalf("fetchFirst error = %v, want errRemoteNotFound", err)
	}
}

func TestImportCommandFromEnvURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# remote\nREMOTE_VAR=hello\n"))
	}))
	t.Cleanup(srv.Close)
	outputPath := setImportOutput(t)

	if err := importCmd.RunE(importCmd, []string{srv.URL + "/.env.example"}); err != nil {
		t.Fatalf("import command failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading generated compose.yml: %v", err)
	}
	if !strings.Contains(string(data), "REMOTE_VAR:") {
		t.Fatalf("expected REMOTE_VAR in output, got:\n%s", data)
	}
}

func TestFetchURLErrors(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantErrPart string
	}{
		{
			name:        "not found",
			handler:     func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
			wantErrPart: "not found",
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
			wantErrPart: "unexpected status",
		},
		{
			name: "html page",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte("<html></html>"))
			},
			wantErrPart: "got HTML",
		},
		{
			name: "too large",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(strings.Repeat("A=1\n", maxRemoteBytes/4+1)))
			},
			wantErrPart: "exceeds",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			t.Cleanup(srv.Close)

			_, err := fetchURL(srv.URL + "/.env")
			if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Fatalf("fetchURL error = %v, want containing %q", err, tt.wantErrPart)
			}
		})
	}
}

func TestImportCommandRejectsComposeAndRepoURLs(t *testing.T) {
	setImportOutput(t)
	for _, src := range []string{
		"https://github.com/front-matter/envy",
		"https://example.org/compose.yml",
	} {
		t.Run(src, func(t *testing.T) {
			err := importCmd.RunE(importCmd, []string{src})
			if err == nil || !strings.Contains(err.Error(), "envy fetch") {
				t.Fatalf("import %s error = %v, want hint to use envy fetch", src, err)
			}
		})
	}
}
