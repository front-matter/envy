package reader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/front-matter/envy/compose"
)

func TestParseComposeEnvValue(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		wantDef  string
		wantEdit bool
		wantReq  bool
	}{
		{name: "literal", key: "A", value: "value", wantDef: "value", wantEdit: false, wantReq: false},
		{name: "editable default", key: "A", value: "${A:-value}", wantDef: "value", wantEdit: true, wantReq: false},
		{name: "editable required", key: "A", value: "${A:?value}", wantDef: "value", wantEdit: true, wantReq: true},
		{name: "editable required no default", key: "A", value: "${A}", wantDef: "", wantEdit: true, wantReq: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			def, editable, required := parseComposeEnvValue(test.key, test.value)
			if def != test.wantDef || editable != test.wantEdit || required != test.wantReq {
				t.Fatalf("got (%q, %v, %v), want (%q, %v, %v)", def, editable, required, test.wantDef, test.wantEdit, test.wantReq)
			}
		})
	}
}

func TestImportCompose(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	composeYAML := `services:
  web:
    image: ghcr.io/example/web:latest
    platform: linux/amd64
    entrypoint: ["/entrypoint.sh"]
    command:
      - gunicorn
      - app:app
    environment:
      APP_ENV: ${APP_ENV:-production}
      APP_DEBUG: false
  worker:
    image: ghcr.io/example/worker:latest
    environment:
      - WORKER_CONCURRENCY=4
      - WORKER_QUEUE
volumes:
  postgres_data:
  redis_data:
    driver: local
networks:
  backend:
  frontend:
    driver: bridge
`

	if err := os.WriteFile(composePath, []byte(composeYAML), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	if len(m.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(m.Services))
	}

	if _, ok := m.Sets["web"]; !ok {
		t.Fatalf("expected web set")
	}
	if _, ok := m.Sets["worker"]; !ok {
		t.Fatalf("expected worker set")
	}

	if len(m.VolumeNames()) != 2 || m.VolumeNames()[0] != "postgres_data" || m.VolumeNames()[1] != "redis_data" {
		t.Fatalf("expected volumes [postgres_data redis_data], got %v", m.VolumeNames())
	}

	if len(m.NetworkNames()) != 2 || m.NetworkNames()[0] != "backend" || m.NetworkNames()[1] != "frontend" {
		t.Fatalf("expected networks [backend frontend], got %v", m.NetworkNames())
	}
}

func TestImportComposeAllowsPWDBindMountWithoutNamedVolume(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	composeYAML := `services:
  ghost:
    image: ghost:latest
    volumes:
      - $PWD/ghost/volumes/config.production.json:/var/lib/ghost/config.production.json
`

	if err := os.WriteFile(composePath, []byte(composeYAML), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	if m == nil {
		t.Fatalf("expected non-nil manifest")
	}

	if len(m.Services) != 1 || m.Services[0].Name != "ghost" {
		t.Fatalf("expected one imported service named ghost, got %#v", m.Services)
	}
}

func TestImportComposeHeaderComments(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	composeYAML := `# My Project
# A multi-line
# description here
services:
  web:
    image: ghcr.io/example/web:latest
    environment:
      APP_ENV: production
`

	if err := os.WriteFile(composePath, []byte(composeYAML), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	if m.Meta.Title != "My Project" {
		t.Fatalf("expected meta.title 'My Project', got %q", m.Meta.Title)
	}

	if m.Meta.Description != "A multi-line\ndescription here" {
		t.Fatalf("expected multi-line description, got %q", m.Meta.Description)
	}
}

func TestImportComposeNoHeaderComments(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	composeYAML := `services:
  web:
    image: ghcr.io/example/web:latest
    environment:
      APP_ENV: production
`

	if err := os.WriteFile(composePath, []byte(composeYAML), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	if m.Meta.Title != "Imported Compose Manifest" {
		t.Fatalf("expected default meta.title, got %q", m.Meta.Title)
	}

	if m.Meta.Description != "" {
		t.Fatalf("expected empty description, got %q", m.Meta.Description)
	}
}

func TestImportComposeWithTopLevelSOPSBlock(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	composeYAML := `sops:
  encrypted_regex: '^(data|stringData)$'
services:
  web:
    image: ghcr.io/example/web:latest
    environment:
      APP_ENV: production
`

	if err := os.WriteFile(composePath, []byte(composeYAML), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	_, err := ImportCompose(composePath)
	if err == nil {
		t.Fatalf("expected schema validation error for top-level sops block")
	}

	message := err.Error()
	if !strings.Contains(message, "additional properties 'sops' not allowed") {
		t.Fatalf("expected compose schema error for sops block, got %q", message)
	}
	if strings.Contains(strings.ToLower(message), "decrypt") {
		t.Fatalf("did not expect decryption path in error, got %q", message)
	}
}

func TestImportComposeMarksSecretsAsSecret(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	composeYAML := `services:
  web:
    image: ghcr.io/example/web:latest
    environment:
      DATABASE_PASSWORD: postgres
      API_SECRET_KEY: secret123
      API_URL: https://api.example.com
`

	if err := os.WriteFile(composePath, []byte(composeYAML), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	webGroup := m.Sets["web"]
	vars := webGroup.Vars

	// Find and verify the secret vars
	var secretVar, apiSecretVar, normalVar *compose.Var
	for i, v := range vars {
		if v.Key == "DATABASE_PASSWORD" {
			secretVar = &vars[i]
		}
		if v.Key == "API_SECRET_KEY" {
			apiSecretVar = &vars[i]
		}
		if v.Key == "API_URL" {
			normalVar = &vars[i]
		}
	}

	if secretVar == nil {
		t.Fatal("expected DATABASE_PASSWORD variable")
	}
	if !secretVar.IsSecret() {
		t.Errorf("DATABASE_PASSWORD should be marked as secret")
	}
	if secretVar.Default != "" {
		t.Errorf("DATABASE_PASSWORD default should be empty for secret var, got %q", secretVar.Default)
	}

	if apiSecretVar == nil {
		t.Fatal("expected API_SECRET_KEY variable")
	}
	if !apiSecretVar.IsSecret() {
		t.Errorf("API_SECRET_KEY should be marked as secret")
	}
	if apiSecretVar.Default != "" {
		t.Errorf("API_SECRET_KEY default should be empty for secret var, got %q", apiSecretVar.Default)
	}

	if normalVar == nil {
		t.Fatal("expected API_URL variable")
	}
	if normalVar.IsSecret() {
		t.Errorf("API_URL should not be marked as secret")
	}
}

func TestImportComposeConsolidatesDuplicateVarsIntoCommonGroup(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	composeYAML := `services:
  web:
    image: ghcr.io/example/web:latest
    environment:
      APP_ENV: ${APP_ENV:-production}
      LOG_LEVEL: info
  worker:
    image: ghcr.io/example/worker:latest
    environment:
      APP_ENV: ${APP_ENV:-production}
      WORKER_CONCURRENCY: "4"
`

	if err := os.WriteFile(composePath, []byte(composeYAML), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	commonGroup, ok := m.Sets["common"]
	if !ok {
		t.Fatalf("expected common set to be created")
	}
	if len(commonGroup.Vars) != 1 || commonGroup.Vars[0].Key != "APP_ENV" {
		t.Fatalf("expected APP_ENV in common set, got %+v", commonGroup.Vars)
	}

	webGroup := m.Sets["web"]
	if len(webGroup.Vars) != 1 || webGroup.Vars[0].Key != "LOG_LEVEL" {
		t.Fatalf("expected LOG_LEVEL to remain in web set, got %+v", webGroup.Vars)
	}

	workerGroup := m.Sets["worker"]
	if len(workerGroup.Vars) != 1 || workerGroup.Vars[0].Key != "WORKER_CONCURRENCY" {
		t.Fatalf("expected WORKER_CONCURRENCY to remain in worker set, got %+v", workerGroup.Vars)
	}

	if len(m.Services[0].Sets) != 2 || m.Services[0].Sets[0] != "common" {
		t.Fatalf("expected first service to include common set, got %+v", m.Services[0].Sets)
	}
	if len(m.Services[1].Sets) != 2 || m.Services[1].Sets[0] != "common" {
		t.Fatalf("expected second service to include common set, got %+v", m.Services[1].Sets)
	}
}

func TestImportComposeKeepsConflictingDuplicateVarsInServiceSets(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	compose := `services:
  web:
    image: ghcr.io/example/web:latest
    environment:
      APP_ENV: ${APP_ENV:-production}
  worker:
    image: ghcr.io/example/worker:latest
    environment:
      APP_ENV: ${APP_ENV:-staging}
`

	if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	if _, ok := m.Sets["common"]; ok {
		t.Fatalf("did not expect common set for conflicting duplicate vars")
	}
	if len(m.Sets["web"].Vars) != 1 || m.Sets["web"].Vars[0].Key != "APP_ENV" {
		t.Fatalf("expected APP_ENV to remain in web set, got %+v", m.Sets["web"].Vars)
	}
	if len(m.Sets["worker"].Vars) != 1 || m.Sets["worker"].Vars[0].Key != "APP_ENV" {
		t.Fatalf("expected APP_ENV to remain in worker set, got %+v", m.Sets["worker"].Vars)
	}
}
