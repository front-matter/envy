package reader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/front-matter/envy/manifest"
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

	compose := `services:
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

	if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	if len(m.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(m.Services))
	}

	if _, ok := m.Groups["web"]; !ok {
		t.Fatalf("expected web group")
	}
	if _, ok := m.Groups["worker"]; !ok {
		t.Fatalf("expected worker group")
	}

	if len(m.Volumes) != 2 || m.Volumes[0] != "postgres_data" || m.Volumes[1] != "redis_data" {
		t.Fatalf("expected volumes [postgres_data redis_data], got %v", m.Volumes)
	}

	if len(m.Networks) != 2 || m.Networks[0] != "backend" || m.Networks[1] != "frontend" {
		t.Fatalf("expected networks [backend frontend], got %v", m.Networks)
	}
}

func TestImportComposeHeaderComments(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	compose := `# My Project
# A multi-line
# description here
services:
  web:
    image: ghcr.io/example/web:latest
    environment:
      APP_ENV: production
`

	if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	if m.Meta.Name != "My Project" {
		t.Fatalf("expected meta.name 'My Project', got %q", m.Meta.Name)
	}

	if m.Meta.Description != "A multi-line\ndescription here" {
		t.Fatalf("expected multi-line description, got %q", m.Meta.Description)
	}
}

func TestImportComposeNoHeaderComments(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	compose := `services:
  web:
    image: ghcr.io/example/web:latest
    environment:
      APP_ENV: production
`

	if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	if m.Meta.Name != "Imported Compose Manifest" {
		t.Fatalf("expected default meta.name, got %q", m.Meta.Name)
	}

	if m.Meta.Description != "" {
		t.Fatalf("expected empty description, got %q", m.Meta.Description)
	}
}

func TestImportComposeMarksSecretsAsSecret(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	compose := `services:
  web:
    image: ghcr.io/example/web:latest
    environment:
      DATABASE_PASSWORD: postgres
      API_SECRET_KEY: secret123
      API_URL: https://api.example.com
`

	if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	webGroup := m.Groups["web"]
	vars := webGroup.Vars

	// Find and verify the secret vars
	var secretVar, apiSecretVar, normalVar *manifest.Var
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
	if !secretVar.Secret {
		t.Errorf("DATABASE_PASSWORD should be marked as secret")
	}

	if apiSecretVar == nil {
		t.Fatal("expected API_SECRET_KEY variable")
	}
	if !apiSecretVar.Secret {
		t.Errorf("API_SECRET_KEY should be marked as secret")
	}

	if normalVar == nil {
		t.Fatal("expected API_URL variable")
	}
	if normalVar.Secret {
		t.Errorf("API_URL should not be marked as secret")
	}
}

func TestImportComposeConsolidatesDuplicateVarsIntoCommonGroup(t *testing.T) {
	tmp := t.TempDir()
	composePath := filepath.Join(tmp, "compose.yaml")

	compose := `services:
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

	if err := os.WriteFile(composePath, []byte(compose), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	m, err := ImportCompose(composePath)
	if err != nil {
		t.Fatalf("ImportCompose() error = %v", err)
	}

	commonGroup, ok := m.Groups["common"]
	if !ok {
		t.Fatalf("expected common group to be created")
	}
	if len(commonGroup.Vars) != 1 || commonGroup.Vars[0].Key != "APP_ENV" {
		t.Fatalf("expected APP_ENV in common group, got %+v", commonGroup.Vars)
	}

	webGroup := m.Groups["web"]
	if len(webGroup.Vars) != 1 || webGroup.Vars[0].Key != "LOG_LEVEL" {
		t.Fatalf("expected LOG_LEVEL to remain in web group, got %+v", webGroup.Vars)
	}

	workerGroup := m.Groups["worker"]
	if len(workerGroup.Vars) != 1 || workerGroup.Vars[0].Key != "WORKER_CONCURRENCY" {
		t.Fatalf("expected WORKER_CONCURRENCY to remain in worker group, got %+v", workerGroup.Vars)
	}

	if len(m.Services[0].Groups) != 2 || m.Services[0].Groups[0] != "common" {
		t.Fatalf("expected first service to include common group, got %+v", m.Services[0].Groups)
	}
	if len(m.Services[1].Groups) != 2 || m.Services[1].Groups[0] != "common" {
		t.Fatalf("expected second service to include common group, got %+v", m.Services[1].Groups)
	}
}

func TestImportComposeKeepsConflictingDuplicateVarsInServiceGroups(t *testing.T) {
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

	if _, ok := m.Groups["common"]; ok {
		t.Fatalf("did not expect common group for conflicting duplicate vars")
	}
	if len(m.Groups["web"].Vars) != 1 || m.Groups["web"].Vars[0].Key != "APP_ENV" {
		t.Fatalf("expected APP_ENV to remain in web group, got %+v", m.Groups["web"].Vars)
	}
	if len(m.Groups["worker"].Vars) != 1 || m.Groups["worker"].Vars[0].Key != "APP_ENV" {
		t.Fatalf("expected APP_ENV to remain in worker group, got %+v", m.Groups["worker"].Vars)
	}
}
