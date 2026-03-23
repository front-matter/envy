package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	types "github.com/compose-spec/compose-go/v2/types"

	"github.com/front-matter/envy/compose"
)

func newImportTestSet(vars types.MappingWithEquals) compose.Set {
	set := compose.NewSet()
	set.SetVars(vars)
	return set
}

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

	want := filepath.Join(folder, "compose.yml")
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
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findImportFiles() = %v, want %v", got, want)
	}
}

func TestFindImportFilesUsesEnvExampleWhenEnvMissing(t *testing.T) {
	tmp := t.TempDir()

	for _, name := range []string{".env.example"} {
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
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findImportFiles() = %v, want %v", got, want)
	}
}

func TestImportCommandPreservesImportedVarsInOutput(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")
	outputPath := filepath.Join(tmp, "compose.yml")

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
	if !strings.Contains(output, "BOOL_TRUE:") {
		t.Fatalf("expected BOOL_TRUE to be preserved, got:\n%s", output)
	}
	if !strings.Contains(output, "BOOL_FALSE:") {
		t.Fatalf("expected BOOL_FALSE to be preserved, got:\n%s", output)
	}
	if strings.Contains(output, "secret:") {
		t.Fatalf("expected no secret fields in generated compose.yaml, got:\n%s", output)
	}
	if !strings.Contains(output, "x-set-env:") {
		t.Fatalf("expected imported env set in generated compose.yaml, got:\n%s", output)
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
			"worker": newImportTestSet(types.MappingWithEquals{"BROKER_URL": nil}),
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
			"worker": newImportTestSet(types.MappingWithEquals{"OTHER_VAR": nil}),
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
