package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/front-matter/envy/compose"
)

func TestResolvePathFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nested", "compose.yaml")

	got, err := resolvePath(path)
	if err != nil {
		t.Fatalf("resolvePath(file): %v", err)
	}

	if got != path {
		t.Fatalf("resolvePath(file) = %q, want %q", got, path)
	}

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("expected parent directory to be created: %v", err)
	}
}

func TestResolvePathFolder(t *testing.T) {
	tmp := t.TempDir()
	folder := filepath.Join(tmp, "out")

	got, err := resolvePath(folder)
	if err != nil {
		t.Fatalf("resolvePath(folder): %v", err)
	}

	want := filepath.Join(folder, "compose.yaml")
	if got != want {
		t.Fatalf("resolvePath(folder) = %q, want %q", got, want)
	}

	info, err := os.Stat(folder)
	if err != nil {
		t.Fatalf("expected output folder to be created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", folder)
	}
}

func TestFileExists(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yaml")

	exists, err := FileExists(path)
	if err != nil {
		t.Fatalf("FileExists(non-existing): %v", err)
	}
	if exists {
		t.Fatalf("expected non-existing file to return exists=false")
	}

	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	exists, err = FileExists(path)
	if err != nil {
		t.Fatalf("FileExists(existing): %v", err)
	}
	if !exists {
		t.Fatalf("expected existing file to return exists=true")
	}
}

func TestFileExistsDirectory(t *testing.T) {
	tmp := t.TempDir()
	dirPath := filepath.Join(tmp, "compose.yaml")

	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}

	exists, err := FileExists(dirPath)
	if err == nil {
		t.Fatalf("expected error when output path is a directory")
	}
	if exists {
		t.Fatalf("directory must not be treated as an existing output file")
	}
}

func TestFindImportFilesOrder(t *testing.T) {
	tmp := t.TempDir()

	files := []string{
		"compose.yml",
		"docker-compose.yaml",
		"docker-compose.yml",
		"compose.yaml",
		".env",
		".env.example",
	}

	for _, name := range files {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	got, err := findImportFiles(tmp)
	if err != nil {
		t.Fatalf("findImportFiles: %v", err)
	}

	want := []string{
		filepath.Join(tmp, ".env"),
		filepath.Join(tmp, "compose.yaml"),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findImportFiles() = %v, want %v", got, want)
	}
}

func TestFindImportFilesUsesEnvExampleWhenEnvMissing(t *testing.T) {
	tmp := t.TempDir()

	for _, name := range []string{".env.example", "docker-compose.yaml"} {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	got, err := findImportFiles(tmp)
	if err != nil {
		t.Fatalf("findImportFiles: %v", err)
	}

	want := []string{
		filepath.Join(tmp, ".env.example"),
		filepath.Join(tmp, "docker-compose.yaml"),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findImportFiles() = %v, want %v", got, want)
	}
}

func TestFindImportFilesComposeFallbackOrder(t *testing.T) {
	tmp := t.TempDir()

	for _, name := range []string{"docker-compose.yml", "compose.yml"} {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	got, err := findImportFiles(tmp)
	if err != nil {
		t.Fatalf("findImportFiles: %v", err)
	}

	want := []string{filepath.Join(tmp, "compose.yml")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findImportFiles() = %v, want %v", got, want)
	}
}

func TestImportCommandOmitsSecretVarsFromOutput(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")
	outputPath := filepath.Join(tmp, "compose.yaml")

	envContent := "BOOL_TRUE=true\nBOOL_FALSE=false\n"
	if err := os.WriteFile(envPath, []byte(envContent), 0o644); err != nil {
		t.Fatalf("writing .env: %v", err)
	}

	oldImportFilePath := importFilePath
	importFilePath = outputPath
	t.Cleanup(func() {
		importFilePath = oldImportFilePath
	})

	if err := importCmd.RunE(importCmd, []string{envPath}); err != nil {
		t.Fatalf("import command failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading generated compose.yaml: %v", err)
	}

	output := string(data)
	if strings.Contains(output, "BOOL_TRUE:") {
		t.Fatalf("expected BOOL_TRUE to be omitted as secret var, got:\n%s", output)
	}
	if strings.Contains(output, "BOOL_FALSE:") {
		t.Fatalf("expected BOOL_FALSE to be omitted as secret var, got:\n%s", output)
	}
	if strings.Contains(output, "secret:") {
		t.Fatalf("expected no secret fields in generated compose.yaml, got:\n%s", output)
	}
	if strings.Contains(output, "sets:") {
		t.Fatalf("expected sets section to be omitted when all vars are secret, got:\n%s", output)
	}
	if !strings.Contains(output, "x-envy:") {
		t.Fatalf("expected meta section in generated compose.yaml, got:\n%s", output)
	}
}

func TestVerifyServiceCommandVarsDefinedOK(t *testing.T) {
	m := &compose.Project{
		Services: []compose.Service{{
			Name:    "worker",
			Command: []string{"--broker=${BROKER_URL:-redis://cache:6379/0}"},
			Sets:    []string{"worker"},
		}},
		Sets: map[string]compose.Set{
			"worker": {
				Vars: []compose.Var{{Key: "BROKER_URL"}},
			},
		},
	}

	if warnings := verifyServiceCommandVarsDefined(m); len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
}

func TestVerifyServiceCommandVarsDefinedMissing(t *testing.T) {
	m := &compose.Project{
		Services: []compose.Service{{
			Name:    "worker",
			Command: []string{"--broker=${BROKER_URL:-redis://cache:6379/0}"},
			Sets:    []string{"worker"},
		}},
		Sets: map[string]compose.Set{
			"worker": {
				Vars: []compose.Var{{Key: "OTHER_VAR"}},
			},
		},
	}

	warnings := verifyServiceCommandVarsDefined(m)
	if len(warnings) == 0 {
		t.Fatalf("expected warning for missing command variable")
	}

	message := warnings[0]
	if !strings.Contains(message, "BROKER_URL") {
		t.Fatalf("expected missing var in warning, got %q", message)
	}
	if !strings.Contains(message, "service \"worker\"") {
		t.Fatalf("expected service name in warning, got %q", message)
	}
}

func TestResolveImportPathsURL(t *testing.T) {
	url := "https://example.org/compose.yaml"

	got, err := resolveImportPaths(url)
	if err != nil {
		t.Fatalf("resolveImportPaths(url): %v", err)
	}

	want := []string{url}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolveImportPaths(url) = %v, want %v", got, want)
	}
}

func TestImportFileFromYAMLURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte("services:\n  web:\n    image: nginx:latest\n"))
	}))
	defer server.Close()

	m, err := importFile(server.URL + "/compose.yaml")
	if err != nil {
		t.Fatalf("importFile(url): %v", err)
	}

	if m == nil {
		t.Fatalf("expected non-nil manifest")
	}

	if len(m.Services) != 1 || m.Services[0].Name != "web" {
		t.Fatalf("expected one imported service named web, got %#v", m.Services)
	}
}
