package compose

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindPrefersComposeYml(t *testing.T) {
	tmp := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(): %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	for _, name := range []string{"compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte("x-envy: {}\n"), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%s): %v", name, err)
		}
	}

	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("os.Chdir(tmp): %v", err)
	}

	got, err := Find()
	if err != nil {
		t.Fatalf("Find(): %v", err)
	}

	if filepath.Base(got) != "compose.yml" {
		t.Fatalf("Find() returned %q, want filename %q", got, "compose.yml")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("Find() returned path that cannot be stat'ed: %v", err)
	}
}

func TestFindFallsBackByOrder(t *testing.T) {
	tests := []struct {
		name      string
		files     []string
		wantFound string
	}{
		{name: "compose.yaml fallback", files: []string{"compose.yaml"}, wantFound: "compose.yaml"},
		{name: "docker-compose.yml fallback", files: []string{"docker-compose.yml"}, wantFound: "docker-compose.yml"},
		{name: "docker-compose.yaml fallback", files: []string{"docker-compose.yaml"}, wantFound: "docker-compose.yaml"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			oldWd, err := os.Getwd()
			if err != nil {
				t.Fatalf("os.Getwd(): %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(oldWd) })

			for _, name := range tc.files {
				if err := os.WriteFile(filepath.Join(tmp, name), []byte("x-envy: {}\n"), 0o644); err != nil {
					t.Fatalf("os.WriteFile(%s): %v", name, err)
				}
			}

			if err := os.Chdir(tmp); err != nil {
				t.Fatalf("os.Chdir(tmp): %v", err)
			}

			got, err := Find()
			if err != nil {
				t.Fatalf("Find(): %v", err)
			}

			if filepath.Base(got) != tc.wantFound {
				t.Fatalf("Find() returned %q, want filename %q", got, tc.wantFound)
			}
			if _, err := os.Stat(got); err != nil {
				t.Fatalf("Find() returned path that cannot be stat'ed: %v", err)
			}
		})
	}
}

func TestFindReturnsHelpfulError(t *testing.T) {
	tmp := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(): %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("os.Chdir(tmp): %v", err)
	}

	_, err = Find()
	if err == nil {
		t.Fatal("Find() expected error, got nil")
	}

	msg := err.Error()
	for _, expected := range []string{"compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml"} {
		if !strings.Contains(msg, expected) {
			t.Fatalf("error %q does not mention %q", msg, expected)
		}
	}
	if !strings.Contains(msg, "--file") {
		t.Fatalf("error %q does not mention --file", msg)
	}
	if strings.Contains(msg, "--manifest") {
		t.Fatalf("error %q should not mention deprecated --manifest flag", msg)
	}
}

func TestFindIn(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		want    string
		wantErr bool
	}{
		{name: "compose.yml", files: []string{"compose.yml"}, want: "compose.yml"},
		{name: "prefers compose.yml", files: []string{"docker-compose.yml", "compose.yml"}, want: "compose.yml"},
		{name: "docker-compose.yaml", files: []string{"docker-compose.yaml"}, want: "docker-compose.yaml"},
		{name: "none", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			// A manifest in the parent must not be found.
			if err := os.WriteFile(filepath.Join(parent, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
				t.Fatalf("os.WriteFile: %v", err)
			}
			dir := filepath.Join(parent, "sub")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatalf("os.Mkdir: %v", err)
			}
			for _, name := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("services: {}\n"), 0o644); err != nil {
					t.Fatalf("os.WriteFile(%s): %v", name, err)
				}
			}

			got, err := FindIn(dir)
			if tt.wantErr {
				if !errors.Is(err, ErrManifestNotFound) {
					t.Fatalf("FindIn() error = %v, want ErrManifestNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FindIn(): %v", err)
			}
			if got != filepath.Join(dir, tt.want) {
				t.Fatalf("FindIn() = %q, want %q", got, filepath.Join(dir, tt.want))
			}
		})
	}
}
