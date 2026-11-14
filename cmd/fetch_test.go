package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func setFetchOutput(t *testing.T) string {
	t.Helper()
	outputPath := filepath.Join(t.TempDir(), "compose.yml")
	old := fetchFilePath
	fetchFilePath = outputPath
	t.Cleanup(func() { fetchFilePath = old })
	return outputPath
}

func TestFetchSourceURL(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr string
	}{
		{in: "https://github.com/front-matter/envy", want: "https://github.com/front-matter/envy"},
		{in: "github.com/front-matter/envy", want: "https://github.com/front-matter/envy"},
		{in: "gitlab.com/group/sub/repo", want: "https://gitlab.com/group/sub/repo"},
		{in: "codeberg.org/org/repo", want: "https://codeberg.org/org/repo"},
		{in: "https://example.org/compose.yml", want: "https://example.org/compose.yml"},
		{in: "https://example.org/.env", wantErr: "envy import"},
		{in: "https://github.com/front-matter", wantErr: "envy import"},
		{in: "./compose.yml", wantErr: "not a URL"},
		{in: "example.org/org/repo", wantErr: "not a URL"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := fetchSourceURL(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("fetchSourceURL(%q) error = %v, want containing %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchSourceURL(%q): %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Fatalf("fetchSourceURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRepoComposeURLs(t *testing.T) {
	tests := []struct {
		in     string
		want   []string
		wantOK bool
	}{
		{
			in: "https://github.com/front-matter/envy",
			want: []string{
				"https://raw.githubusercontent.com/front-matter/envy/HEAD/compose.yml",
				"https://raw.githubusercontent.com/front-matter/envy/HEAD/compose.yaml",
			},
			wantOK: true,
		},
		{
			in: "https://github.com/front-matter/envy.git/",
			want: []string{
				"https://raw.githubusercontent.com/front-matter/envy/HEAD/compose.yml",
				"https://raw.githubusercontent.com/front-matter/envy/HEAD/compose.yaml",
			},
			wantOK: true,
		},
		{
			in: "https://codeberg.org/org/repo",
			want: []string{
				"https://codeberg.org/api/v1/repos/org/repo/raw/compose.yml",
				"https://codeberg.org/api/v1/repos/org/repo/raw/compose.yaml",
			},
			wantOK: true,
		},
		{
			in: "https://gitlab.com/group/sub/repo",
			want: []string{
				"https://gitlab.com/api/v4/projects/group%2Fsub%2Frepo/repository/files/compose.yml/raw?ref=HEAD",
				"https://gitlab.com/api/v4/projects/group%2Fsub%2Frepo/repository/files/compose.yaml/raw?ref=HEAD",
			},
			wantOK: true,
		},
		{in: "https://github.com/front-matter", wantOK: false},
		{in: "https://github.com/front-matter/envy/blob/main/compose.yml", wantOK: false},
		{in: "https://gitlab.com/group/repo/-/raw/main/compose.yml", wantOK: false},
		{in: "https://example.org/org/repo", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			u, err := url.Parse(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := repoComposeURLs(u)
			if ok != tt.wantOK || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("repoComposeURLs(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestFetchCommandFromComposeURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(testCompose))
	}))
	t.Cleanup(srv.Close)
	outputPath := setFetchOutput(t)

	if err := fetchCmd.RunE(fetchCmd, []string{srv.URL + "/stack/compose.yaml"}); err != nil {
		t.Fatalf("fetch command failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading written compose.yml: %v", err)
	}
	if string(data) != testCompose {
		t.Fatalf("expected compose file written unchanged, got:\n%s", data)
	}
}

func TestFetchCommandDoesNotOverwrite(t *testing.T) {
	outputPath := setFetchOutput(t)
	if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fetchCmd.RunE(fetchCmd, []string{"https://example.invalid/compose.yml"}); err != nil {
		t.Fatalf("fetch command failed: %v", err)
	}

	data, _ := os.ReadFile(outputPath)
	if string(data) != "existing" {
		t.Fatalf("expected existing file untouched, got %q", data)
	}
}

func TestFetchComposeRejectsInvalidYAML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("services: [\n"))
	}))
	t.Cleanup(srv.Close)
	outputPath := setFetchOutput(t)

	u, _ := url.Parse(srv.URL + "/compose.yml")
	if err := fetchCompose(u, outputPath); err == nil {
		t.Fatal("expected error for invalid compose YAML")
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("expected no output file, stat err = %v", err)
	}
}
