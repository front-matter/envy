package reader

import (
	"path/filepath"
	"testing"

	"github.com/front-matter/envy/envfile"
	"github.com/front-matter/envy/manifest"
)

func TestImportEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	// Create test .env file
	envContent := `# This is a comment
DEBUG=true
DATABASE_URL=postgres://localhost:5432/mydb
PORT=8080
REDIS_URL=redis://localhost:6379
APP_NAME=TestApp
`

	if err := envfile.Write(envPath, envContent); err != nil {
		t.Fatalf("failed to write test .env file: %v", err)
	}

	m, err := ImportEnvFile(envPath)
	if err != nil {
		t.Fatalf("ImportEnvFile failed: %v", err)
	}

	// Verify manifest structure
	if m.Meta.Title != "Imported Env Manifest" {
		t.Errorf("expected name 'Imported Env Manifest', got %s", m.Meta.Title)
	}

	if len(m.Sets) != 1 {
		t.Errorf("expected 1 set, got %d", len(m.Sets))
	}

	set, ok := m.Sets["env"]
	if !ok {
		t.Errorf("expected 'env' set")
	}

	if len(set.Vars) != 5 {
		t.Errorf("expected 5 vars, got %d", len(set.Vars))
	}

	for _, v := range set.Vars {
		if v.Key == "APP_NAME" && v.Default != "" {
			t.Errorf("expected APP_NAME default to be empty for secret var, got %s", v.Default)
		}
	}
}

func TestMergeManifests(t *testing.T) {
	env1 := &manifest.Manifest{
		Meta: manifest.Meta{Title: "env1", Version: "v1"},
		Sets: map[string]manifest.Set{
			"db": {
				Vars: []manifest.Var{
					{Key: "DB_HOST", Default: "localhost"},
					{Key: "DB_PORT", Default: "5432"},
				},
			},
		},
	}

	env2 := &manifest.Manifest{
		Meta: manifest.Meta{Title: "env2", Version: "v1"},
		Services: []manifest.Service{
			{Name: "web", Image: "nginx:latest"},
		},
		Sets: map[string]manifest.Set{
			"web": {
				Vars: []manifest.Var{
					{Key: "APP_NAME", Default: "myapp"},
				},
			},
			"db": {
				Vars: []manifest.Var{
					{Key: "DB_HOST", Default: "db.example.com"},
					{Key: "DB_USER", Default: "admin"},
				},
			},
		},
	}

	merged := Merge(env1, env2)

	if len(merged.Services) != 1 {
		t.Errorf("expected 1 service in merged manifest, got %d", len(merged.Services))
	}

	if len(merged.Sets) != 2 {
		t.Errorf("expected 2 sets in merged manifest, got %d", len(merged.Sets))
	}

	dbGroup := merged.Sets["db"]
	if len(dbGroup.Vars) != 3 {
		t.Errorf("expected 3 vars in db set after merge, got %d", len(dbGroup.Vars))
	}

	// Check that env2's version of DB_HOST (db.example.com) is used
	for _, v := range dbGroup.Vars {
		if v.Key == "DB_HOST" && v.Default != "db.example.com" {
			t.Errorf("expected DB_HOST to be overridden to 'db.example.com', got %s", v.Default)
		}
	}
}

func TestMergeEmptyManifests(t *testing.T) {
	m1 := &manifest.Manifest{
		Meta: manifest.Meta{Title: "m1", Version: "v1"},
		Sets: map[string]manifest.Set{
			"app": {
				Vars: []manifest.Var{
					{Key: "SETTING_1"},
				},
			},
		},
	}

	merged := Merge(m1)
	if merged == nil {
		t.Error("Merge must return non-nil manifest")
	}

	if len(merged.Sets["app"].Vars) != 1 {
		t.Errorf("expected 1 var in app set, got %d", len(merged.Sets["app"].Vars))
	}

	// Test merge with no manifests
	emptyMerge := Merge()
	if emptyMerge == nil {
		t.Error("Merge with no args should return empty manifest, not nil")
	}
	if len(emptyMerge.Sets) != 0 {
		t.Errorf("empty merge should have no sets, got %d", len(emptyMerge.Sets))
	}
}

func TestMergeThreeManifests(t *testing.T) {
	m1 := &manifest.Manifest{
		Meta: manifest.Meta{Title: "m1", Version: "v1"},
		Sets: map[string]manifest.Set{
			"app": {
				Vars: []manifest.Var{
					{Key: "VAR_1", Default: "value1"},
				},
			},
		},
	}

	m2 := &manifest.Manifest{
		Meta: manifest.Meta{Title: "m2", Version: "v1"},
		Sets: map[string]manifest.Set{
			"app": {
				Vars: []manifest.Var{
					{Key: "VAR_2", Default: "value2"},
				},
			},
		},
	}

	m3 := &manifest.Manifest{
		Meta: manifest.Meta{Title: "m3", Version: "v1"},
		Sets: map[string]manifest.Set{
			"app": {
				Vars: []manifest.Var{
					{Key: "VAR_1", Default: "value1_updated"},
					{Key: "VAR_3", Default: "value3"},
				},
			},
		},
	}

	merged := Merge(m1, m2, m3)

	appGroup := merged.Sets["app"]
	if len(appGroup.Vars) != 3 {
		t.Errorf("expected 3 vars after merging 3 manifests, got %d", len(appGroup.Vars))
	}

	// Check that last source wins for VAR_1
	for _, v := range appGroup.Vars {
		if v.Key == "VAR_1" && v.Default != "value1_updated" {
			t.Errorf("expected VAR_1 to be 'value1_updated' from m3, got %s", v.Default)
		}
	}
}

func TestMergeSkipsEnvVarsAlreadyPresentInComposeSets(t *testing.T) {
	compose := &manifest.Manifest{
		Meta: manifest.Meta{Title: "compose", Version: "v1"},
		Services: []manifest.Service{
			{Name: "web", Sets: []string{"web"}},
		},
		Sets: map[string]manifest.Set{
			"web": {
				Vars: []manifest.Var{
					{Key: "APP_ENV", Default: "production"},
					{Key: "DB_HOST", Default: "db"},
				},
			},
		},
	}

	envFile := &manifest.Manifest{
		Meta: manifest.Meta{Title: "env", Version: "v1"},
		Sets: map[string]manifest.Set{
			"env": {
				Vars: []manifest.Var{
					{Key: "APP_ENV", Default: "local"},
					{Key: "EXTRA_ONLY", Default: "1"},
				},
			},
		},
	}

	merged := Merge(compose, envFile)

	envGroup := merged.Sets["env"]
	if len(envGroup.Vars) != 1 {
		t.Fatalf("expected only 1 env var after dedupe, got %d", len(envGroup.Vars))
	}
	if envGroup.Vars[0].Key != "EXTRA_ONLY" {
		t.Fatalf("expected EXTRA_ONLY to remain in env set, got %s", envGroup.Vars[0].Key)
	}

	webGroup := merged.Sets["web"]
	for _, v := range webGroup.Vars {
		if v.Key == "APP_ENV" && v.Default != "production" {
			t.Fatalf("expected compose APP_ENV to be preserved, got %s", v.Default)
		}
	}
}
