package reader

import (
	"path/filepath"
	"testing"

	types "github.com/compose-spec/compose-go/v2/types"

	"github.com/front-matter/envy/compose"
	"github.com/front-matter/envy/envfile"
)

func newReaderTestSet(vars types.MappingWithEquals, configure ...func(*compose.Set)) compose.Set {
	set := compose.NewSet()
	set.SetVars(vars)
	for _, fn := range configure {
		fn(&set)
	}
	return set
}

func strPtr(value string) *string {
	v := value
	return &v
}

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

	if len(set.Vars()) != 5 {
		t.Errorf("expected 5 vars, got %d", len(set.Vars()))
	}

	if got := compose.VarString(set.Vars()["APP_NAME"]); got != "TestApp" {
		t.Errorf("expected APP_NAME default to be preserved, got %s", got)
	}
}

func TestMergeManifests(t *testing.T) {
	env1 := &compose.Project{
		Meta: compose.Meta{Title: "env1", Version: "v1"},
		Sets: map[string]compose.Set{
			"db": newReaderTestSet(types.MappingWithEquals{"DB_HOST": strPtr("localhost"), "DB_PORT": strPtr("5432")}),
		},
	}

	env2 := &compose.Project{
		Meta: compose.Meta{Title: "env2", Version: "v1"},
		Services: []compose.Service{
			{Name: "web", Image: "nginx:latest"},
		},
		Sets: map[string]compose.Set{
			"web": newReaderTestSet(types.MappingWithEquals{"APP_NAME": strPtr("myapp")}),
			"db":  newReaderTestSet(types.MappingWithEquals{"DB_HOST": strPtr("db.example.com"), "DB_USER": strPtr("admin")}),
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
	if len(dbGroup.Vars()) != 3 {
		t.Errorf("expected 3 vars in db set after merge, got %d", len(dbGroup.Vars()))
	}

	// Check that env2's version of DB_HOST (db.example.com) is used
	if got := compose.VarString(dbGroup.Vars()["DB_HOST"]); got != "db.example.com" {
		t.Errorf("expected DB_HOST to be overridden to 'db.example.com', got %s", got)
	}
}

func TestMergeEmptyManifests(t *testing.T) {
	m1 := &compose.Project{
		Meta: compose.Meta{Title: "m1", Version: "v1"},
		Sets: map[string]compose.Set{
			"app": newReaderTestSet(types.MappingWithEquals{"SETTING_1": strPtr("")}),
		},
	}

	merged := Merge(m1)
	if merged == nil {
		t.Error("Merge must return non-nil manifest")
	}

	if len(merged.Sets["app"].Vars()) != 1 {
		t.Errorf("expected 1 var in app set, got %d", len(merged.Sets["app"].Vars()))
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
	m1 := &compose.Project{
		Meta: compose.Meta{Title: "m1", Version: "v1"},
		Sets: map[string]compose.Set{
			"app": newReaderTestSet(types.MappingWithEquals{"VAR_1": strPtr("value1")}),
		},
	}

	m2 := &compose.Project{
		Meta: compose.Meta{Title: "m2", Version: "v1"},
		Sets: map[string]compose.Set{
			"app": newReaderTestSet(types.MappingWithEquals{"VAR_2": strPtr("value2")}),
		},
	}

	m3 := &compose.Project{
		Meta: compose.Meta{Title: "m3", Version: "v1"},
		Sets: map[string]compose.Set{
			"app": newReaderTestSet(types.MappingWithEquals{"VAR_1": strPtr("value1_updated"), "VAR_3": strPtr("value3")}),
		},
	}

	merged := Merge(m1, m2, m3)

	appGroup := merged.Sets["app"]
	if len(appGroup.Vars()) != 3 {
		t.Errorf("expected 3 vars after merging 3 manifests, got %d", len(appGroup.Vars()))
	}

	// Check that last source wins for VAR_1
	if got := compose.VarString(appGroup.Vars()["VAR_1"]); got != "value1_updated" {
		t.Errorf("expected VAR_1 to be 'value1_updated' from m3, got %s", got)
	}
}

func TestMergeSkipsEnvVarsAlreadyPresentInComposeSets(t *testing.T) {
	composeManifest := &compose.Project{
		Meta: compose.Meta{Title: "compose", Version: "v1"},
		Services: []compose.Service{
			{Name: "web", Sets: []string{"web"}},
		},
		Sets: map[string]compose.Set{
			"web": newReaderTestSet(types.MappingWithEquals{"APP_ENV": strPtr("production"), "DB_HOST": strPtr("db")}),
		},
	}

	envFile := &compose.Project{
		Meta: compose.Meta{Title: "env", Version: "v1"},
		Sets: map[string]compose.Set{
			"env": newReaderTestSet(types.MappingWithEquals{"APP_ENV": strPtr("local"), "EXTRA_ONLY": strPtr("1")}),
		},
	}

	merged := Merge(composeManifest, envFile)

	envGroup := merged.Sets["env"]
	if len(envGroup.Vars()) != 1 {
		t.Fatalf("expected only 1 env var after dedupe, got %d", len(envGroup.Vars()))
	}
	if _, ok := envGroup.Vars()["EXTRA_ONLY"]; !ok {
		t.Fatalf("expected EXTRA_ONLY to remain in env set")
	}
	if _, ok := envGroup.Vars()["APP_ENV"]; ok {
		t.Fatalf("expected APP_ENV to be removed from env set")
	}

	webGroup := merged.Sets["web"]
	if got := compose.VarString(webGroup.Vars()["APP_ENV"]); got != "production" {
		t.Fatalf("expected compose APP_ENV to be preserved, got %s", got)
	}
}
