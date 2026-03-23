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
	if m.Meta.Title != "Imported Env Manifest" {
		t.Errorf("expected 'Imported Env Manifest', got %s", m.Meta.Title)
	}

	set, ok := m.Sets["env"]
	if !ok {
		t.Error("expected 'env' set")
	}

	if len(set.Vars()) != 3 {
		t.Errorf("expected 3 variables, got %d", len(set.Vars()))
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

func TestImportEnvFilePreservesAllValuesAsStrings(t *testing.T) {
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

	set := m.Sets["env"]
	for _, v := range set.Vars() {
		if got := v.Default; got != mappableEnvValue(v.Key) {
			t.Errorf("%s: expected default %q, got %q", v.Key, mappableEnvValue(v.Key), got)
		}
	}
}

func mappableEnvValue(key string) string {
	switch key {
	case "BOOL_TRUE":
		return "true"
	case "BOOL_FALSE":
		return "false"
	case "INT_VALUE":
		return "42"
	case "URL_REDIS":
		return "redis://localhost:6379"
	case "URL_DB":
		return "postgres://user:pass@db:5432/db"
	case "STRING_VALUE":
		return "hello world"
	case "EMPTY_VALUE":
		return ""
	default:
		return ""
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

	set, ok := m.Sets["env"]
	if !ok {
		t.Error("expected 'env' set")
	}

	if len(set.Vars()) != 1 {
		t.Errorf("expected 1 variable, got %d", len(set.Vars()))
	}

	if set.Vars()[0].Key != "LOCAL_VAR" {
		t.Errorf("expected variable LOCAL_VAR, got %s", set.Vars()[0].Key)
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

	set := m.Sets["env"]

	// Verify variables are sorted
	expected := []string{"APPLE", "BANANA", "MONKEY", "ZEBRA"}
	for i, v := range set.Vars() {
		if v.Key != expected[i] {
			t.Errorf("variable %d: expected %s, got %s", i, expected[i], v.Key)
		}
	}
}
