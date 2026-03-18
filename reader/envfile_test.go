package reader

import (
	"path/filepath"
	"testing"

	"github.com/front-matter/envy/envfile"
)

func TestImportEnvFileBasic(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	envContent := `DEBUG=true
PORT=3000
DATABASE_URL=postgres://localhost/mydb
`

	if err := envfile.Write(envPath, envContent); err != nil {
		t.Fatalf("failed to write .env file: %v", err)
	}

	m, err := ImportEnvFile(envPath)
	if err != nil {
		t.Fatalf("ImportEnvFile failed: %v", err)
	}

	// Verify manifest
	if m.Meta.Name != "Imported Env Manifest" {
		t.Errorf("expected 'Imported Env Manifest', got %s", m.Meta.Name)
	}

	group, ok := m.Groups["env"]
	if !ok {
		t.Error("expected 'env' group")
	}

	if len(group.Vars) != 3 {
		t.Errorf("expected 3 variables, got %d", len(group.Vars))
	}
}

func TestImportEnvFileEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	if err := envfile.Write(envPath, "# Only comments\n"); err != nil {
		t.Fatalf("failed to write .env file: %v", err)
	}

	_, err := ImportEnvFile(envPath)
	if err == nil {
		t.Error("expected error for empty .env file")
	}
}

func TestImportEnvFileNotFound(t *testing.T) {
	_, err := ImportEnvFile("/nonexistent/.env")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestImportEnvFileTreatsAllVarsAsStrings(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	envContent := `BOOL_TRUE=true
BOOL_FALSE=false
INT_VALUE=42
URL_REDIS=redis://localhost:6379
URL_DB=postgres://user:pass@db:5432/db
STRING_VALUE=hello world
EMPTY_VALUE=
`

	if err := envfile.Write(envPath, envContent); err != nil {
		t.Fatalf("failed to write .env file: %v", err)
	}

	m, err := ImportEnvFile(envPath)
	if err != nil {
		t.Fatalf("ImportEnvFile failed: %v", err)
	}

	group := m.Groups["env"]
	for _, v := range group.Vars {
		if v.Default == "" && v.Key != "EMPTY_VALUE" {
			t.Errorf("%s: expected default value to be preserved", v.Key)
		}
	}
}

func TestImportEnvFileEnvLocal(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env.local")

	envContent := `LOCAL_VAR=local_value
`

	if err := envfile.Write(envPath, envContent); err != nil {
		t.Fatalf("failed to write .env.local file: %v", err)
	}

	m, err := ImportEnvFile(envPath)
	if err != nil {
		t.Fatalf("ImportEnvFile(.env.local) failed: %v", err)
	}

	group, ok := m.Groups["env"]
	if !ok {
		t.Error("expected 'env' group")
	}

	if len(group.Vars) != 1 {
		t.Errorf("expected 1 variable, got %d", len(group.Vars))
	}

	if group.Vars[0].Key != "LOCAL_VAR" {
		t.Errorf("expected variable LOCAL_VAR, got %s", group.Vars[0].Key)
	}
}

func TestImportEnvFileSorted(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	// Write in non-alphabetical order
	envContent := `ZEBRA=z
APPLE=a
MONKEY=m
BANANA=b
`

	if err := envfile.Write(envPath, envContent); err != nil {
		t.Fatalf("failed to write .env file: %v", err)
	}

	m, err := ImportEnvFile(envPath)
	if err != nil {
		t.Fatalf("ImportEnvFile failed: %v", err)
	}

	group := m.Groups["env"]

	// Verify variables are sorted
	expected := []string{"APPLE", "BANANA", "MONKEY", "ZEBRA"}
	for i, v := range group.Vars {
		if v.Key != expected[i] {
			t.Errorf("variable %d: expected %s, got %s", i, expected[i], v.Key)
		}
	}
}
