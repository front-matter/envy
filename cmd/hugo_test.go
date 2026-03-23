package cmd

import (
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/front-matter/envy/compose"
	"gopkg.in/yaml.v3"
)

func newHugoTestSet(vars []compose.Var, configure ...func(*compose.Set)) compose.Set {
	set := compose.NewSet()
	set.SetVars(vars)
	for _, fn := range configure {
		fn(&set)
	}
	return set
}

func TestNormalizeSetDocLink(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLabel  string
		wantTarget string
	}{
		{
			name:       "plain url",
			input:      "https://example.org/common",
			wantLabel:  "https://example.org/common",
			wantTarget: "https://example.org/common",
		},
		{
			name:       "markdown reference style",
			input:      "[Common Docs]: https://example.org/common",
			wantLabel:  "Common Docs",
			wantTarget: "https://example.org/common",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, target := normalizeSetDocLink(tt.input)
			if label != tt.wantLabel || target != tt.wantTarget {
				t.Fatalf("normalizeSetDocLink(%q) = (%q, %q), want (%q, %q)", tt.input, label, target, tt.wantLabel, tt.wantTarget)
			}
		})
	}
}

func TestSplitServiceDescriptionAndLink(t *testing.T) {
	description, link := splitServiceDescriptionAndLink("Describes search service configuration. For details see\nhttps://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/")
	if description != "Describes search service configuration. For details see" {
		t.Fatalf("expected description without link line, got %q", description)
	}
	if link != "https://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/" {
		t.Fatalf("expected extracted link, got %q", link)
	}
}

func TestRenderServiceCardUsesDescriptionLink(t *testing.T) {
	service := compose.Service{
		Name:        "search",
		Description: "Describes search service configuration. For details see\nhttps://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/",
	}

	card := renderServiceCard(service, "en", "/services/search")
	if !strings.Contains(card, "description=`Describes search service configuration. For details see`") {
		t.Fatalf("expected service card description without doc link line, got:\n%s", card)
	}
	if !strings.Contains(card, "descriptionLink=\"https://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/\"") {
		t.Fatalf("expected service card to emit descriptionLink, got:\n%s", card)
	}
	if strings.Contains(card, "description=`Describes search service configuration. For details see\nhttps://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/`") {
		t.Fatalf("expected service card description to not include URL line, got:\n%s", card)
	}
}

func TestHasFlag(t *testing.T) {
	args := []string{"--destination", "public", "--cleanDestinationDir", "--baseURL=https://example.org"}
	if !hasFlag(args, "--cleanDestinationDir") {
		t.Fatal("expected --cleanDestinationDir to be detected")
	}
	if !hasFlag(args, "--baseURL") {
		t.Fatal("expected --baseURL=... to be detected")
	}
	if hasFlag(args, "--renderToMemory") {
		t.Fatal("did not expect unrelated flag to be detected")
	}
}

func TestRunHugoCommandBuildAbortsOnValidationError(t *testing.T) {
	originalRunner := composeConfigRunner
	originalManifestPath := manifestPath
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		composeConfigRunner = originalRunner
		manifestPath = originalManifestPath
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("failed to restore cwd: %v", chdirErr)
		}
	})

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "compose.yml"), []byte("x-envy:\n  title: Example\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir(%s) error = %v", tmp, err)
	}

	composeConfigRunner = func(_ string) (string, error) {
		return "validation failed in test", errors.New("exit status 1")
	}
	manifestPath = ""

	err = runHugoCommand("build", nil)
	if err == nil {
		t.Fatalf("expected build to abort on validation error")
	}
	if !strings.Contains(err.Error(), "envy build aborted: validation failed") {
		t.Fatalf("expected build abort prefix, got: %v", err)
	}
	if !strings.Contains(err.Error(), "validation failed in test") {
		t.Fatalf("expected wrapped validate output, got: %v", err)
	}
}

func TestParseWatchFlag(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantWatch bool
		wantArgs  []string
		wantErr   bool
	}{
		{
			name:      "watch enabled by standalone flag",
			args:      []string{"--bind", "0.0.0.0", "--watch"},
			wantWatch: true,
			wantArgs:  []string{"--bind", "0.0.0.0"},
		},
		{
			name:      "watch enabled by explicit true",
			args:      []string{"--watch=true", "--bind", "0.0.0.0"},
			wantWatch: true,
			wantArgs:  []string{"--bind", "0.0.0.0"},
		},
		{
			name:      "watch disabled by explicit false",
			args:      []string{"--watch=false", "--bind", "0.0.0.0"},
			wantWatch: false,
			wantArgs:  []string{"--bind", "0.0.0.0"},
		},
		{
			name:    "invalid watch value errors",
			args:    []string{"--watch=maybe"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			watchEnabled, filtered, err := parseWatchFlag(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for args %v", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWatchFlag(%v) error = %v", tt.args, err)
			}
			if watchEnabled != tt.wantWatch {
				t.Fatalf("parseWatchFlag(%v) watch = %v, want %v", tt.args, watchEnabled, tt.wantWatch)
			}
			if len(filtered) != len(tt.wantArgs) {
				t.Fatalf("parseWatchFlag(%v) filtered len = %d, want %d (%v)", tt.args, len(filtered), len(tt.wantArgs), filtered)
			}
			for i := range filtered {
				if filtered[i] != tt.wantArgs[i] {
					t.Fatalf("parseWatchFlag(%v) filtered[%d] = %q, want %q", tt.args, i, filtered[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestComposeManifestChangedDetectsSizeChange(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	if err := os.WriteFile(path, []byte("x-envy:\n  title: Example\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prev, err := manifestState(path)
	if err != nil {
		t.Fatalf("manifestState() error = %v", err)
	}

	if err := os.WriteFile(path, []byte("x-envy:\n  title: Example Updated\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(updated) error = %v", err)
	}

	changed, _, err := manifestChanged(path, prev)
	if err != nil {
		t.Fatalf("manifestChanged() error = %v", err)
	}
	if !changed {
		t.Fatalf("expected size change to be detected")
	}
}

func TestComposeManifestChangedNoChange(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	if err := os.WriteFile(path, []byte("x-envy:\n  title: Example\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prev, err := manifestState(path)
	if err != nil {
		t.Fatalf("manifestState() error = %v", err)
	}

	// Ensure filesystem timestamp can advance if needed on slower timestamp resolutions.
	time.Sleep(10 * time.Millisecond)

	changed, _, err := manifestChanged(path, prev)
	if err != nil {
		t.Fatalf("manifestChanged() error = %v", err)
	}
	if changed {
		t.Fatalf("expected no change to be detected")
	}
}

func TestUpdateComposeServiceFieldInManifest(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  # old description\n  db:\n    image: postgres:17\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := updateServiceFieldInManifest(path, "db", "description", "updated description"); err != nil {
		t.Fatalf("updateServiceFieldInManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "# updated description") {
		t.Fatalf("expected updated service description, got:\n%s", updated)
	}
}

func TestUpdateComposeServiceFieldInManifestRenamesService(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  # db description\n  db:\n    image: postgres:17\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := updateServiceFieldInManifest(path, "db", "name", "database"); err != nil {
		t.Fatalf("updateServiceFieldInManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if !strings.Contains(updatedContent, "database:") {
		t.Fatalf("expected renamed service key, got:\n%s", updatedContent)
	}
	if strings.Contains(updatedContent, "\ndb:\n") || strings.Contains(updatedContent, "\n    db:\n") {
		t.Fatalf("expected old service key to be gone, got:\n%s", updatedContent)
	}
	if !strings.Contains(updatedContent, "# db description") {
		t.Fatalf("expected service comments to remain attached, got:\n%s", updatedContent)
	}
}

func TestUpdateComposeServiceFieldInManifestRejectsSlugCollision(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  db:\n    image: postgres:17\n  my-service:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	err := updateServiceFieldInManifest(path, "db", "name", "my service")
	if err == nil {
		t.Fatalf("expected service rename collision error")
	}
	if !strings.Contains(err.Error(), "conflicts with existing service") {
		t.Fatalf("expected collision message, got: %v", err)
	}
}

func TestUpdateComposeSetFieldInManifest(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("# old description\n# Link: https://old.example.org\nx-set-mail: &mail\n  MAIL_HOST: localhost\nservices:\n  web:\n    image: caddy:2.10\n    environment:\n      <<: [*mail]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := updateSetFieldInManifest(path, "mail", "link", "https://new.example.org"); err != nil {
		t.Fatalf("updateSetFieldInManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "# Link: https://new.example.org") {
		t.Fatalf("expected updated set link, got:\n%s", updated)
	}
}

func TestUpdateComposeSetFieldInManifestRenamesSet(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	// The anchor (&mail) and alias (*mail) should both be updated on rename.
	content := []byte("# Mail description\nx-set-mail: &mail\n  MAIL_HOST: localhost\nx-set-cache: &cache\n  CACHE_URL: redis://localhost\nservices:\n  web:\n    image: caddy:2.10\n    environment:\n      <<: [*mail]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := updateSetFieldInManifest(path, "mail", "name", "smtp"); err != nil {
		t.Fatalf("updateSetFieldInManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if !strings.Contains(updatedContent, "x-set-smtp:") {
		t.Fatalf("expected renamed set key, got:\n%s", updatedContent)
	}
	if strings.Contains(updatedContent, "x-set-mail:") {
		t.Fatalf("expected old set key to be gone, got:\n%s", updatedContent)
	}
	if !strings.Contains(updatedContent, "# Mail description") {
		t.Fatalf("expected set comments to remain attached, got:\n%s", updatedContent)
	}
	if !strings.Contains(updatedContent, "x-set-cache:") {
		t.Fatalf("expected unrelated set to be unchanged, got:\n%s", updatedContent)
	}
}

func TestUpdateComposeSetFieldInManifestRejectsSlugCollision(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-mail: &mail\n  MAIL_HOST: localhost\nx-set-my-cache: &my-cache\n  CACHE_URL: redis://localhost\nservices:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	err := updateSetFieldInManifest(path, "mail", "name", "my cache")
	if err == nil {
		t.Fatalf("expected set rename collision error")
	}
	if !strings.Contains(err.Error(), "conflicts with existing set") {
		t.Fatalf("expected collision message, got: %v", err)
	}
}

func TestComposeSetAPIHandlerRedirectsAfterRename(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-mail: &mail\n  MAIL_HOST: localhost\nservices:\n  web:\n    image: caddy:2.10\n    environment:\n      <<: [*mail]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "name")
	form.Set("page", "/sets/mail/")
	form.Set("key", "mail")
	form.Set("value", "smtp")

	req := httptest.NewRequest(http.MethodPost, "/api/sets", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	setAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("setAPIHandler() status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/sets/smtp/" {
		t.Fatalf("setAPIHandler() HX-Redirect = %q, want %q", got, "/sets/smtp/")
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "x-set-smtp:") {
		t.Fatalf("expected renamed set in manifest, got:\n%s", updated)
	}
}

func TestDeleteSetFromManifestRemovesSetAndAliases(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-base: &base\n  BASE_VAR: value\nx-set-authentication: &authentication\n  AUTH_VAR: value\nservices:\n  web:\n    image: caddy:2.10\n    environment:\n      <<: [*base, *authentication]\n  worker:\n    image: busybox\n    environment:\n      <<: [*base]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := deleteSetFromManifest(path, "base"); err != nil {
		t.Fatalf("deleteSetFromManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if strings.Contains(updatedContent, "x-set-base:") {
		t.Fatalf("expected deleted set to be removed, got:\n%s", updatedContent)
	}
	if strings.Contains(updatedContent, "*base") {
		t.Fatalf("expected deleted set aliases to be removed, got:\n%s", updatedContent)
	}
	if !strings.Contains(updatedContent, "*authentication") {
		t.Fatalf("expected other set aliases to remain, got:\n%s", updatedContent)
	}
}

func TestDeleteSetFromManifestRejectsMissingSet(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-base: &base\n  BASE_VAR: value\nservices:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	err := deleteSetFromManifest(path, "missing")
	if err == nil {
		t.Fatal("expected error for missing set")
	}
	if !strings.Contains(err.Error(), "set \"missing\" not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddSetToManifestAddsNewTopLevelSet(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-base: &base\n  BASE_VAR: value\nservices:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := addSetToManifest(path, "cache"); err != nil {
		t.Fatalf("addSetToManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if !strings.Contains(updatedContent, "x-set-cache: &cache") {
		t.Fatalf("expected new set to be added, got:\n%s", updatedContent)
	}
	if strings.Index(updatedContent, "x-set-cache:") > strings.Index(updatedContent, "services:") {
		t.Fatalf("expected new set before services block, got:\n%s", updatedContent)
	}
}

func TestAddSetToManifestRejectsDuplicate(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-cache: &cache\n  CACHE_URL: redis://localhost\nservices:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	err := addSetToManifest(path, "cache")
	if err == nil {
		t.Fatal("expected duplicate set error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate message, got: %v", err)
	}
}

func TestAddServiceToManifestAddsNewService(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := addServiceToManifest(path, "worker"); err != nil {
		t.Fatalf("addServiceToManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if !strings.Contains(updatedContent, "worker:") {
		t.Fatalf("expected new service to be added, got:\n%s", updatedContent)
	}
	if !strings.Contains(updatedContent, "busybox:latest") {
		t.Fatalf("expected new service to get default image, got:\n%s", updatedContent)
	}
}

func TestAddServiceToManifestRejectsDuplicate(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  my-service:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	err := addServiceToManifest(path, "my service")
	if err == nil {
		t.Fatal("expected duplicate service error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate message, got: %v", err)
	}
}

func TestDeleteServiceFromManifestRemovesService(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n  db:\n    image: postgres:17\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := deleteServiceFromManifest(path, "db"); err != nil {
		t.Fatalf("deleteServiceFromManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if strings.Contains(updatedContent, "\n    db:\n") {
		t.Fatalf("expected deleted service to be removed, got:\n%s", updatedContent)
	}
	if !strings.Contains(updatedContent, "\n    web:\n") {
		t.Fatalf("expected other services to remain, got:\n%s", updatedContent)
	}
}

func TestDeleteServiceFromManifestRejectsMissingService(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	err := deleteServiceFromManifest(path, "missing")
	if err == nil {
		t.Fatal("expected error for missing service")
	}
	if !strings.Contains(err.Error(), "service \"missing\" not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceAPIHandlerCreatesNewService(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "create")
	form.Set("value", "db")

	req := httptest.NewRequest(http.MethodPost, "/api/services", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	serviceAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("serviceAPIHandler() status = %d, want %d; body: %s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/services/db/" {
		t.Fatalf("serviceAPIHandler() HX-Redirect = %q, want %q", got, "/services/db/")
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "\n    db:\n") {
		t.Fatalf("expected new service in manifest, got:\n%s", updated)
	}
}

func TestServiceAPIHandlerCreatesNewServiceWithoutFieldOrPage(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("value", "db")

	req := httptest.NewRequest(http.MethodPost, "/api/services", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	serviceAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("serviceAPIHandler() status = %d, want %d; body: %s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/services/db/" {
		t.Fatalf("serviceAPIHandler() HX-Redirect = %q, want %q", got, "/services/db/")
	}
}

func TestComposeServiceAPIHandlerDeletesServiceAndRedirectsToLocalizedIndex(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n  db:\n    image: postgres:17\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/services/db/?page=/de/services/db/", nil)
	resp := httptest.NewRecorder()

	serviceAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("serviceAPIHandler() status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/de/services/" {
		t.Fatalf("serviceAPIHandler() HX-Redirect = %q, want %q", got, "/de/services/")
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if strings.Contains(string(updated), "\n    db:\n") {
		t.Fatalf("expected deleted service to be removed, got:\n%s", updated)
	}
}

func TestComposeServiceAPIHandlerDeleteRejectsNonDetailPage(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  db:\n    image: postgres:17\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/services/db/?page=/services/", nil)
	resp := httptest.NewRecorder()

	serviceAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("serviceAPIHandler() status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
	if !strings.Contains(resp.Body.String(), "service deletion is only allowed from service detail pages") {
		t.Fatalf("expected detail page error, got: %s", resp.Body.String())
	}
}

func TestComposeServiceAPIHandlerDeleteRejectsMismatchedSlug(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  db:\n    image: postgres:17\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/services/db/?page=/services/web/", nil)
	resp := httptest.NewRecorder()

	serviceAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("serviceAPIHandler() status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
	if !strings.Contains(resp.Body.String(), "service slug does not match service detail page") {
		t.Fatalf("expected slug mismatch error, got: %s", resp.Body.String())
	}
}

func TestComposeSetAPIHandlerDeletesSetAndRedirectsToLocalizedIndex(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-mail: &mail\n  MAIL_HOST: localhost\nservices:\n  web:\n    image: caddy:2.10\n    environment:\n      <<: [*mail]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/sets/mail/?page=/de/sets/mail/", nil)
	resp := httptest.NewRecorder()

	setAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("setAPIHandler() status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/de/sets/" {
		t.Fatalf("setAPIHandler() HX-Redirect = %q, want %q", got, "/de/sets/")
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if strings.Contains(updatedContent, "x-set-mail:") {
		t.Fatalf("expected deleted set to be removed, got:\n%s", updatedContent)
	}
	if strings.Contains(updatedContent, "*mail") {
		t.Fatalf("expected deleted set alias to be removed, got:\n%s", updatedContent)
	}
}

func TestSetAPIHandlerCreatesNewSet(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-base: &base\n  BASE_VAR: value\nservices:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "create")
	form.Set("value", "cache")

	req := httptest.NewRequest(http.MethodPost, "/api/sets", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	setAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("setAPIHandler() status = %d, want %d; body: %s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/sets/cache/" {
		t.Fatalf("setAPIHandler() HX-Redirect = %q, want %q", got, "/sets/cache/")
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "x-set-cache: &cache") {
		t.Fatalf("expected new set in manifest, got:\n%s", updated)
	}
}

func TestSetAPIHandlerCreatesNewSetWithoutFieldOrPage(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-base: &base\n  BASE_VAR: value\nservices:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("value", "cache")

	req := httptest.NewRequest(http.MethodPost, "/api/sets", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	setAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("setAPIHandler() status = %d, want %d; body: %s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/sets/cache/" {
		t.Fatalf("setAPIHandler() HX-Redirect = %q, want %q", got, "/sets/cache/")
	}
}

func TestSetAPIHandlerCreateRejectsDuplicate(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-cache: &cache\n  CACHE_URL: redis://localhost\nservices:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "create")
	form.Set("value", "cache")

	req := httptest.NewRequest(http.MethodPost, "/api/sets", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	setAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("setAPIHandler() status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
}

func TestComposeSetAPIHandlerDeleteRejectsNonDetailPage(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-mail: &mail\n  MAIL_HOST: localhost\nservices:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/sets/mail/?page=/sets/", nil)
	resp := httptest.NewRecorder()

	setAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("setAPIHandler() status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
	if !strings.Contains(resp.Body.String(), "set deletion is only allowed from set detail pages") {
		t.Fatalf("expected detail page error, got: %s", resp.Body.String())
	}
}

func TestComposeSetAPIHandlerDeleteRejectsMismatchedSlug(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-mail: &mail\n  MAIL_HOST: localhost\nservices:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/sets/mail/?page=/sets/cache/", nil)
	resp := httptest.NewRecorder()

	setAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("setAPIHandler() status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
	if !strings.Contains(resp.Body.String(), "set slug does not match set detail page") {
		t.Fatalf("expected slug mismatch error, got: %s", resp.Body.String())
	}
}

func TestComposeProfileAPIHandlerRejectsRenameForNoneProfile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "name")
	form.Set("page", "/profiles/none/")
	form.Set("key", "none")
	form.Set("value", "default")

	req := httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	profileAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("profileAPIHandler() status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
	if !strings.Contains(resp.Body.String(), "profile \"none\" is virtual and cannot be renamed or deleted") {
		t.Fatalf("expected virtual profile error, got: %s", resp.Body.String())
	}
}

func TestAddProfileToManifestAddsToExistingProfilesSequence(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := addProfileToManifest(path, "staging"); err != nil {
		t.Fatalf("addProfileToManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "- staging") {
		t.Fatalf("expected new profile in manifest, got:\n%s", updated)
	}
}

func TestAddProfileToManifestAddsToFirstServiceWhenNoProfiles(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n  db:\n    image: postgres:17\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := addProfileToManifest(path, "dev"); err != nil {
		t.Fatalf("addProfileToManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "- dev") {
		t.Fatalf("expected new profile in manifest, got:\n%s", updated)
	}
}

func TestAddProfileToManifestRejectsDuplicate(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	err := addProfileToManifest(path, "dev")
	if err == nil {
		t.Fatal("addProfileToManifest() expected error for duplicate profile")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got: %v", err)
	}
}

func TestAddProfileToManifestRejectsNoneName(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := addProfileToManifest(path, "none"); err == nil {
		t.Fatal("addProfileToManifest() expected error for reserved name 'none'")
	}
}

func TestProfileAPIHandlerCreatesNewProfile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "create")
	form.Set("value", "staging")

	req := httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	profileAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("profileAPIHandler() status = %d, want %d; body: %s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/profiles/staging/" {
		t.Fatalf("profileAPIHandler() HX-Redirect = %q, want %q", got, "/profiles/staging/")
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "- staging") {
		t.Fatalf("expected new profile in manifest, got:\n%s", updated)
	}
}

func TestProfileAPIHandlerCreateRejectsDuplicate(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "create")
	form.Set("value", "dev")

	req := httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	profileAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("profileAPIHandler() status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
}

func TestProfileAPIHandlerCreatesNewProfileWithoutFieldOrPage(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("value", "staging")

	req := httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	profileAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("profileAPIHandler() status = %d, want %d; body: %s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/profiles/staging/" {
		t.Fatalf("profileAPIHandler() HX-Redirect = %q, want %q", got, "/profiles/staging/")
	}
}

func TestProfileAPIHandlerCreateMultipartRequest(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	body := &strings.Builder{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("field", "create"); err != nil {
		t.Fatalf("WriteField(field) error = %v", err)
	}
	if err := writer.WriteField("value", "staging"); err != nil {
		t.Fatalf("WriteField(value) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() multipart writer error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp := httptest.NewRecorder()

	profileAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("profileAPIHandler() status = %d, want %d; body: %s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
}

func TestProfileAPIHandlerCreatesNewProfileRunsRefreshHook(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "create")
	form.Set("value", "staging")

	called := false
	req := httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	profileAPIHandler(path, func() error {
		called = true
		return nil
	}).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("profileAPIHandler() status = %d, want %d; body: %s", resp.Code, http.StatusNoContent, resp.Body.String())
	}
	if !called {
		t.Fatal("expected refresh hook to be called")
	}
}

func TestUpdateComposeSetFieldInManifestRenamesBaseInComposeLikeManifest(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-envy:\n  HUGO_TITLE: Example\n\n# Base defines shared environment variables.\n# Link: https://example.org/base\nx-set-base: &base\n  BASE_VAR: value\nx-set-authentication: &authentication\n  AUTH_VAR: value\nservices:\n  web:\n    image: caddy:2.10\n    environment:\n      !!merge <<: [*base, *authentication]\n  worker:\n    image: busybox\n    environment:\n      !!merge <<: [*base]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := updateSetFieldInManifest(path, "base", "name", "common"); err != nil {
		t.Fatalf("updateSetFieldInManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if !strings.Contains(updatedContent, "x-set-common:") {
		t.Fatalf("expected renamed base key, got:\n%s", updatedContent)
	}
	if strings.Contains(updatedContent, "x-set-base:") {
		t.Fatalf("expected old base key to be removed, got:\n%s", updatedContent)
	}
}

func TestUpdateComposeSetFieldMultiAliasRename(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-set-base: &base\n  FOO: bar\nx-set-cache: &cache\n  BAR: baz\nservices:\n  web:\n    image: caddy:2.10\n    environment:\n      <<: [*base, *cache]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := updateSetFieldInManifest(path, "base", "name", "common"); err != nil {
		t.Fatalf("updateSetFieldInManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if !strings.Contains(updatedContent, "x-set-common:") {
		t.Fatalf("expected renamed set key, got:\n%s", updatedContent)
	}
	if strings.Contains(updatedContent, "x-set-base:") {
		t.Fatalf("expected old set key to be gone, got:\n%s", updatedContent)
	}
	if !strings.Contains(updatedContent, "x-set-cache:") {
		t.Fatalf("expected unrelated set to remain, got:\n%s", updatedContent)
	}
}

func TestUpdateComposeSetFieldWithMergeTagRename(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	// Uses !!merge <<: syntax as found in the real compose.yml
	content := []byte("x-set-base: &base\n  FOO: bar\nx-set-cache: &cache\n  BAR: baz\nservices:\n  web:\n    image: caddy:2.10\n    environment:\n      !!merge <<: [*base, *cache]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := updateSetFieldInManifest(path, "base", "name", "common"); err != nil {
		t.Fatalf("updateSetFieldInManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if !strings.Contains(updatedContent, "x-set-common:") {
		t.Fatalf("expected renamed set key, got:\n%s", updatedContent)
	}
	if strings.Contains(updatedContent, "x-set-base:") {
		t.Fatalf("expected old set key to be gone, got:\n%s", updatedContent)
	}
}

func TestUpdateComposeSetFieldFirstSetAfterBlankLine(t *testing.T) {
	// Reproduces the real compose.yml structure: x-set-base follows x-envy with a blank line before its comment.
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("x-envy:\n  HUGO_TITLE: Example\n\n# Base description\n# Link: https://old.example.org\nx-set-base: &base\n  FOO: bar\nservices:\n  web:\n    image: caddy:2.10\n    environment:\n      !!merge <<: [*base]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := updateSetFieldInManifest(path, "base", "link", "https://new.example.org"); err != nil {
		t.Fatalf("updateSetFieldInManifest() link error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "https://new.example.org") {
		t.Fatalf("expected updated link, got:\n%s", updated)
	}
}

func TestUpdateComposeProfileFieldInManifest(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n      - staging\n  worker:\n    image: ghcr.io/example/worker:latest\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := updateProfileFieldInManifest(path, "dev", "name", "development"); err != nil {
		t.Fatalf("updateProfileFieldInManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if !strings.Contains(updatedContent, "- development") {
		t.Fatalf("expected sequence profile to be renamed, got:\n%s", updatedContent)
	}
	if strings.Contains(updatedContent, "\n            - dev\n") {
		t.Fatalf("expected old profile name to be gone, got:\n%s", updatedContent)
	}
}

func TestDeleteProfileFromManifestRemovesProfileReferences(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n      - staging\n  worker:\n    image: ghcr.io/example/worker:latest\n    profiles: dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	if err := deleteProfileFromManifest(path, "dev"); err != nil {
		t.Fatalf("deleteProfileFromManifest() error = %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if strings.Contains(updatedContent, "- dev") {
		t.Fatalf("expected sequence profile to be removed, got:\n%s", updatedContent)
	}
	if strings.Contains(updatedContent, "profiles: dev") {
		t.Fatalf("expected scalar profile to be removed, got:\n%s", updatedContent)
	}
	if !strings.Contains(updatedContent, "- staging") {
		t.Fatalf("expected other profile to remain, got:\n%s", updatedContent)
	}
}

func TestDeleteProfileFromManifestRejectsMissingProfile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	err := deleteProfileFromManifest(path, "staging")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	if !strings.Contains(err.Error(), "profile \"staging\" not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdatedEntityPagePath(t *testing.T) {
	tests := []struct {
		name     string
		pagePath string
		section  string
		newSlug  string
		want     string
		wantErr  bool
	}{
		{
			name:     "profile page",
			pagePath: "/profiles/dev/",
			section:  "profiles",
			newSlug:  "development",
			want:     "/profiles/development/",
		},
		{
			name:     "localized profile page",
			pagePath: "/de/profiles/dev/",
			section:  "profiles",
			newSlug:  "entwicklung",
			want:     "/de/profiles/entwicklung/",
		},
		{
			name:     "wrong section",
			pagePath: "/services/db/",
			section:  "profiles",
			newSlug:  "development",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := updatedEntityPagePath(tt.pagePath, tt.section, tt.newSlug)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("updatedEntityPagePath(%q, %q, %q) expected error", tt.pagePath, tt.section, tt.newSlug)
				}
				return
			}
			if err != nil {
				t.Fatalf("updatedEntityPagePath(%q, %q, %q) error = %v", tt.pagePath, tt.section, tt.newSlug, err)
			}
			if got != tt.want {
				t.Fatalf("updatedEntityPagePath(%q, %q, %q) = %q, want %q", tt.pagePath, tt.section, tt.newSlug, got, tt.want)
			}
		})
	}
}

func TestComposeProfileAPIHandlerRedirectsAfterRename(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "name")
	form.Set("page", "/profiles/dev/")
	form.Set("key", "dev")
	form.Set("value", "development")

	req := httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	profileAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("profileAPIHandler() status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/profiles/development/" {
		t.Fatalf("profileAPIHandler() HX-Redirect = %q, want %q", got, "/profiles/development/")
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "- development") {
		t.Fatalf("expected renamed profile in manifest, got:\n%s", updated)
	}
}

func TestComposeProfileAPIHandlerDeletesProfileAndRedirectsToLocalizedIndex(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n      - staging\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/profiles/dev/?page=/de/profiles/dev/", nil)
	resp := httptest.NewRecorder()

	profileAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("profileAPIHandler() status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/de/profiles/" {
		t.Fatalf("profileAPIHandler() HX-Redirect = %q, want %q", got, "/de/profiles/")
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	updatedContent := string(updated)
	if strings.Contains(updatedContent, "- dev") {
		t.Fatalf("expected deleted profile to be removed, got:\n%s", updatedContent)
	}
	if !strings.Contains(updatedContent, "- staging") {
		t.Fatalf("expected remaining profile to be preserved, got:\n%s", updatedContent)
	}
}

func TestComposeProfileAPIHandlerDeleteRejectsNonDetailPage(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/profiles/dev/?page=/profiles/", nil)
	resp := httptest.NewRecorder()

	profileAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("profileAPIHandler() status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
	if !strings.Contains(resp.Body.String(), "profile deletion is only allowed from profile detail pages") {
		t.Fatalf("expected detail page error, got: %s", resp.Body.String())
	}
}

func TestComposeProfileAPIHandlerDeleteRejectsMismatchedSlug(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  web:\n    image: caddy:2.10\n    profiles:\n      - dev\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/profiles/dev/?page=/profiles/staging/", nil)
	resp := httptest.NewRecorder()

	profileAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("profileAPIHandler() status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
	if !strings.Contains(resp.Body.String(), "profile slug does not match profile detail page") {
		t.Fatalf("expected slug mismatch error, got: %s", resp.Body.String())
	}
}

func TestComposeServiceAPIHandlerRedirectsAfterRename(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "compose.yml")
	content := []byte("services:\n  db:\n    image: postgres:17\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml) error = %v", err)
	}

	form := url.Values{}
	form.Set("field", "name")
	form.Set("page", "/services/db/")
	form.Set("key", "db")
	form.Set("value", "database")

	req := httptest.NewRequest(http.MethodPost, "/api/services", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()

	serviceAPIHandler(path).ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("serviceAPIHandler() status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if got := resp.Header().Get("HX-Redirect"); got != "/services/database/" {
		t.Fatalf("serviceAPIHandler() HX-Redirect = %q, want %q", got, "/services/database/")
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(compose.yml) error = %v", err)
	}
	if !strings.Contains(string(updated), "database:") {
		t.Fatalf("expected renamed service in manifest, got:\n%s", updated)
	}
}

func TestPrepareBuildContentDirCopiesExistingContentAndGeneratesGroupPages(t *testing.T) {
	siteRoot := t.TempDir()
	existingContentDir := filepath.Join(siteRoot, persistentContentDirName)
	if err := os.MkdirAll(existingContentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(content): %v", err)
	}
	if err := os.WriteFile(filepath.Join(existingContentDir, "about.md"), []byte("# About\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(about.md): %v", err)
	}

	m := &compose.Project{
		Meta: compose.Meta{
			Title:               "Example",
			Description:         "Example description",
			Version:             "v1",
			HugoDefaultLanguage: "en",
			HugoLanguages:       "en:\n  languageName: English\n  weight: 1\nde:\n  languageName: Deutsch\n  weight: 2\n",
		},
		Services: []compose.Service{{
			Name:     "web",
			Image:    "caddy:2.10",
			Platform: "linux/amd64",
			Command:  []string{"caddy", "run", "--config", "/etc/caddy/Caddyfile"},
			Profiles: []string{"public", "debug"},
			Sets:     []string{"common"},
		}, {
			Name:        "api",
			Image:       "nginx:1.27",
			Description: "Internal API service.",
		}, {
			Name:        "worker",
			Image:       "busybox:1.36",
			Description: "Background worker.",
			Profiles:    []string{"internal"},
		}},
		Sets: map[string]compose.Set{
			"common": newHugoTestSet(
				[]compose.Var{{Key: "ZZZ_LAST", Default: "z"}, {Key: "APP_ENV", Default: "production"}, {Key: "TEST_REQUIRED_PREFIX_VAR", Default: "?required-value"}, {Key: "TEST_VISIBLE_VAR", Default: "visible-value"}, {Key: "TEST_READONLY_VAR", Default: "locked-value"}},
				func(set *compose.Set) {
					set.SetDescription("Shared settings for runtime services.")
					set.SetLink("[Common Docs]: https://example.org/common")
				},
			),
		},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	if _, err := os.Stat(filepath.Join(contentDir, "about.md")); !os.IsNotExist(err) {
		t.Fatalf("expected stale content file to be removed during refresh, got: %v", err)
	}

	indexContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/_index.md): %v", err)
	}
	if !strings.Contains(string(indexContent), "title=\"common\"") {
		t.Fatalf("expected generated sets index to render a card for common, got:\n%s", string(indexContent))
	}
	if !strings.Contains(string(indexContent), "cardType=\"set\"") {
		t.Fatalf("expected generated sets index to render set shortcode icon, got:\n%s", string(indexContent))
	}
	indexChecks := []string{
		"titleLink=\"/sets/common/\"",
		"description=`Shared settings for runtime services.`",
		"descriptionLink=\"https://example.org/common\"",
		"tagsServices=\"web\"",
	}
	for _, check := range indexChecks {
		if !strings.Contains(string(indexContent), check) {
			t.Fatalf("expected generated sets index to contain %q, got:\n%s", check, string(indexContent))
		}
	}
	if !strings.Contains(string(indexContent), "name: Sets") {
		t.Fatalf("expected generated sets index to include menu metadata, got:\n%s", string(indexContent))
	}
	if !strings.Contains(string(indexContent), "{{< cards cols=\"2\" >}}") {
		t.Fatalf("expected generated sets index to include cards shortcode, got:\n%s", string(indexContent))
	}

	homeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.md): %v", err)
	}
	homeChecks := []string{
		"Example description",
	}
	for _, check := range homeChecks {
		if !strings.Contains(string(homeContent), check) {
			t.Fatalf("expected generated home page to contain %q, got:\n%s", check, string(homeContent))
		}
	}

	homeNotChecks := []string{
		"## Overview",
		"## Navigation",
		"[Browse all sets](/sets/)",
		"{{< cards cols=\"2\" >}}",
		"{{< cards cols=\"3\" >}}",
		"title=\"Browse all sets\"",
		"title=\"common\"",
		"/sets/common/",
	}
	for _, check := range homeNotChecks {
		if strings.Contains(string(homeContent), check) {
			t.Fatalf("expected generated home page to not contain %q, got:\n%s", check, string(homeContent))
		}
	}

	groupContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "common.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/common.md): %v", err)
	}
	checks := []string{
		"Shared settings for runtime services.",
		"https://example.org/common",
		"title=\"APP_ENV\"",
		"title=\"TEST_VISIBLE_VAR\"",
		"cardType=\"var\"",
		`<div id="app_env">`,
		"title=\"common\"",
		"cardType=\"set\"",
		"toc: false",
		"description=`Shared settings for runtime services.`",
		"descriptionLink=\"https://example.org/common\"",
		"tagsServices=\"web\"",
		`var="production"`,
		`var="required-value"`,
		`var="visible-value"`,
		`var="locked-value"`,
	}
	for _, check := range checks {
		if !strings.Contains(string(groupContent), check) {
			t.Fatalf("expected generated set page to contain %q, got:\n%s", check, string(groupContent))
		}
	}
	if strings.Contains(string(groupContent), "link=\"#app_env\"") {
		t.Fatalf("expected generated set page variable cards to be non-clickable, got:\n%s", string(groupContent))
	}
	if !strings.Contains(string(groupContent), "visible-value") {
		t.Fatalf("expected generated set page to include visible defaults, got:\n%s", string(groupContent))
	}

	localizedSetsIndexContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/_index.de.md): %v", err)
	}
	localizedSetsChecks := []string{
		"title: Sets",
		"description: Automatisch generierte Referenz der Konfigurations-Sets aus compose.yml.",
		"name: Sets",
	}
	for _, check := range localizedSetsChecks {
		if !strings.Contains(string(localizedSetsIndexContent), check) {
			t.Fatalf("expected generated localized sets index to contain %q, got:\n%s", check, string(localizedSetsIndexContent))
		}
	}

	localizedGroupContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "common.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/common.de.md): %v", err)
	}
	localizedGroupChecks := []string{
		"Erforderlich",
		"descriptionLink=\"https://example.org/common\"",
		"tagsServices=\"web\"",
		`var="locked-value"`,
	}
	for _, check := range localizedGroupChecks {
		if !strings.Contains(string(localizedGroupContent), check) {
			t.Fatalf("expected generated localized set page to contain %q, got:\n%s", check, string(localizedGroupContent))
		}
	}

	servicesIndexContent, err := os.ReadFile(filepath.Join(contentDir, "services", "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(services/_index.md): %v", err)
	}
	servicesChecks := []string{
		"title: Services",
		"hide: true",
		"name: Services",
		"{{< add-service >}}",
		"title=\"web\"",
		"titleLink=\"/services/web\"",
		"cardType=\"service\"",
		"dockerImage=\"caddy:2.10\"",
		"dockerImageLink=\"https://hub.docker.com/_/caddy\"",
		"platform=\"linux/amd64\"",
		"command=\"[ \\\"caddy\\\", \\\"run\\\", \\\"--config\\\", \\\"/etc/caddy/Caddyfile\\\" ]\"",
		"tagsSets=\"common\"",
		"tagsProfiles=\"public,debug\"",
		"[ \\\"caddy\\\", \\\"run\\\", \\\"--config\\\", \\\"/etc/caddy/Caddyfile\\\" ]",
	}
	for _, check := range servicesChecks {
		if !strings.Contains(string(servicesIndexContent), check) {
			t.Fatalf("expected generated services index to contain %q, got:\n%s", check, string(servicesIndexContent))
		}
	}
	if strings.Contains(string(servicesIndexContent), `link="/services/`) {
		t.Fatalf("expected generated services index cards to avoid outer service links, got:\n%s", string(servicesIndexContent))
	}

	localizedServicesIndexContent, err := os.ReadFile(filepath.Join(contentDir, "services", "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(services/_index.de.md): %v", err)
	}
	localizedServicesChecks := []string{
		"title: Dienste",
		"name: Dienste",
		"dockerImage=\"caddy:2.10\"",
		"dockerImageLink=\"https://hub.docker.com/_/caddy\"",
		"platform=\"linux/amd64\"",
		"command=\"[ \\\"caddy\\\", \\\"run\\\", \\\"--config\\\", \\\"/etc/caddy/Caddyfile\\\" ]\"",
	}
	for _, check := range localizedServicesChecks {
		if !strings.Contains(string(localizedServicesIndexContent), check) {
			t.Fatalf("expected generated localized services index to contain %q, got:\n%s", check, string(localizedServicesIndexContent))
		}
	}
	if strings.Contains(string(localizedServicesIndexContent), `link="/services/`) {
		t.Fatalf("expected generated localized services index cards to avoid outer service links, got:\n%s", string(localizedServicesIndexContent))
	}

	profilesIndexContent, err := os.ReadFile(filepath.Join(contentDir, "profiles", "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(profiles/_index.md): %v", err)
	}
	profilesChecks := []string{
		"title: Profiles",
		"description: Auto-generated profile reference from compose.yml.",
		"title=\"none\"",
		"title=\"debug\"",
		"title=\"public\"",
		"cardType=\"profile\"",
		"link=\"/profiles/none/\"",
		"link=\"/profiles/debug/\"",
		"link=\"/profiles/public/\"",
	}
	for _, check := range profilesChecks {
		if !strings.Contains(string(profilesIndexContent), check) {
			t.Fatalf("expected generated profiles index to contain %q, got:\n%s", check, string(profilesIndexContent))
		}
	}

	profileContent, err := os.ReadFile(filepath.Join(contentDir, "profiles", "public.md"))
	if err != nil {
		t.Fatalf("ReadFile(profiles/public.md): %v", err)
	}
	profileChecks := []string{
		"title: public",
		"hideTitle: true",
		"toc: false",
		"{{< cards cols=\"1\" >}}",
		"cardType=\"profile\"",
		"title=\"public\"",
		"title=\"web\"",
		"titleLink=\"/services/web\"",
		"dockerImage=\"caddy:2.10\"",
		"dockerImageLink=\"https://hub.docker.com/_/caddy\"",
		"platform=\"linux/amd64\"",
		"title=\"api\"",
		"titleLink=\"/services/api\"",
		"description=`Internal API service.`",
	}
	for _, check := range profileChecks {
		if !strings.Contains(string(profileContent), check) {
			t.Fatalf("expected generated profile page to contain %q, got:\n%s", check, string(profileContent))
		}
	}
	if strings.Contains(string(profileContent), `link="/services/`) {
		t.Fatalf("expected generated profile service cards to avoid outer service links, got:\n%s", string(profileContent))
	}
	if strings.Contains(string(profileContent), `titleLink="/services/worker"`) {
		t.Fatalf("expected generated profile page to exclude services from other profiles, got:\n%s", string(profileContent))
	}

	noneProfileContent, err := os.ReadFile(filepath.Join(contentDir, "profiles", "none.md"))
	if err != nil {
		t.Fatalf("ReadFile(profiles/none.md): %v", err)
	}
	noneProfileChecks := []string{
		"title: none",
		"hideTitle: true",
		"toc: false",
		"{{< cards cols=\"1\" >}}",
		"cardType=\"profile\"",
		"title=\"none\"",
		"title=\"api\"",
		"titleLink=\"/services/api\"",
		"description=`Internal API service.`",
		"dockerImage=\"nginx:1.27\"",
		"dockerImageLink=\"https://hub.docker.com/_/nginx\"",
	}
	for _, check := range noneProfileChecks {
		if !strings.Contains(string(noneProfileContent), check) {
			t.Fatalf("expected generated none profile page to contain %q, got:\n%s", check, string(noneProfileContent))
		}
	}
	if strings.Contains(string(noneProfileContent), `link="/services/`) {
		t.Fatalf("expected generated none-profile service cards to avoid outer service links, got:\n%s", string(noneProfileContent))
	}
	if strings.Contains(string(noneProfileContent), `titleLink="/services/web"`) {
		t.Fatalf("expected generated none profile page to exclude profiled services, got:\n%s", string(noneProfileContent))
	}
	if strings.Contains(string(noneProfileContent), `titleLink="/services/worker"`) {
		t.Fatalf("expected generated none profile page to exclude services from other profiles, got:\n%s", string(noneProfileContent))
	}

	localizedProfilesIndexContent, err := os.ReadFile(filepath.Join(contentDir, "profiles", "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(profiles/_index.de.md): %v", err)
	}
	localizedProfilesChecks := []string{
		"title: Profile",
		"description: Automatisch generierte Profilreferenz aus compose.yml.",
		"title=\"none\"",
	}
	for _, check := range localizedProfilesChecks {
		if !strings.Contains(string(localizedProfilesIndexContent), check) {
			t.Fatalf("expected generated localized profiles index to contain %q, got:\n%s", check, string(localizedProfilesIndexContent))
		}
	}

	localizedProfileContent, err := os.ReadFile(filepath.Join(contentDir, "profiles", "public.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(profiles/public.de.md): %v", err)
	}
	localizedProfileChecks := []string{
		"title: public",
		"{{< cards cols=\"1\" >}}",
		"cardType=\"profile\"",
		"title=\"public\"",
		"title=\"web\"",
		"titleLink=\"/services/web\"",
		"title=\"api\"",
		"titleLink=\"/services/api\"",
	}
	for _, check := range localizedProfileChecks {
		if !strings.Contains(string(localizedProfileContent), check) {
			t.Fatalf("expected generated localized profile page to contain %q, got:\n%s", check, string(localizedProfileContent))
		}
	}

	localizedNoneProfileContent, err := os.ReadFile(filepath.Join(contentDir, "profiles", "none.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(profiles/none.de.md): %v", err)
	}
	localizedNoneProfileChecks := []string{
		"title: none",
		"{{< cards cols=\"1\" >}}",
		"cardType=\"profile\"",
		"title=\"none\"",
		"title=\"api\"",
		"titleLink=\"/services/api\"",
	}
	for _, check := range localizedNoneProfileChecks {
		if !strings.Contains(string(localizedNoneProfileContent), check) {
			t.Fatalf("expected generated localized none profile page to contain %q, got:\n%s", check, string(localizedNoneProfileContent))
		}
	}

	serviceContent, err := os.ReadFile(filepath.Join(contentDir, "services", "web.md"))
	if err != nil {
		t.Fatalf("ReadFile(services/web.md): %v", err)
	}
	serviceChecks := []string{
		"title: web",
		"toc: false",
		"title=\"web\"",
		"tagsSets=\"common\"",
		"tagsProfiles=\"public,debug\"",
		"title=\"APP_ENV\"",
		"cardType=\"var\"",
		"title=\"TEST_VISIBLE_VAR\"",
		"title=\"TEST_READONLY_VAR\"",
		"title=\"ZZZ_LAST\"",
	}
	for _, check := range serviceChecks {
		if !strings.Contains(string(serviceContent), check) {
			t.Fatalf("expected generated service page to contain %q, got:\n%s", check, string(serviceContent))
		}
	}

	appEnvPos := strings.Index(string(serviceContent), "title=\"APP_ENV\"")
	readonlyPos := strings.Index(string(serviceContent), "title=\"TEST_READONLY_VAR\"")
	zzzLastPos := strings.Index(string(serviceContent), "title=\"ZZZ_LAST\"")
	if appEnvPos == -1 || readonlyPos == -1 || zzzLastPos == -1 {
		t.Fatalf("expected APP_ENV, TEST_READONLY_VAR and ZZZ_LAST cards in service page, got:\n%s", string(serviceContent))
	}
	if !(appEnvPos < readonlyPos && readonlyPos < zzzLastPos) {
		t.Fatalf("expected service vars sorted alphabetically, got order positions APP_ENV=%d TEST_READONLY_VAR=%d ZZZ_LAST=%d", appEnvPos, readonlyPos, zzzLastPos)
	}
	if !strings.Contains(string(serviceContent), "visible-value") {
		t.Fatalf("expected generated service page to include visible defaults, got:\n%s", string(serviceContent))
	}

	localizedServiceContent, err := os.ReadFile(filepath.Join(contentDir, "services", "web.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(services/web.de.md): %v", err)
	}
	if !strings.Contains(string(localizedServiceContent), "title=\"APP_ENV\"") {
		t.Fatalf("expected localized generated service page to contain service vars, got:\n%s", string(localizedServiceContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}
func TestPrepareBuildContentDirRefreshesExistingGroupPage(t *testing.T) {
	siteRoot := t.TempDir()
	groupDir := filepath.Join(siteRoot, persistentContentDirName, "sets")
	if err := os.MkdirAll(groupDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(sets): %v", err)
	}
	customPage := "# Custom\n"
	if err := os.WriteFile(filepath.Join(groupDir, "common.md"), []byte(customPage), 0o644); err != nil {
		t.Fatalf("WriteFile(common.md): %v", err)
	}

	m := &compose.Project{
		Sets: map[string]compose.Set{
			"common": newHugoTestSet(nil, func(set *compose.Set) {
				set.SetDescription("Shared settings.")
			}),
		},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	groupContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "common.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/common.md): %v", err)
	}
	if string(groupContent) == customPage {
		t.Fatalf("expected existing set page to be refreshed, got:\n%s", string(groupContent))
	}
	if !strings.Contains(string(groupContent), "Shared settings.") {
		t.Fatalf("expected refreshed set page content, got:\n%s", string(groupContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestPrepareBuildContentDirWithOptionsKeepsStaleFilesWhenRefreshDisabled(t *testing.T) {
	siteRoot := t.TempDir()
	contentDir := filepath.Join(siteRoot, persistentContentDirName)
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(content): %v", err)
	}
	stalePath := filepath.Join(contentDir, "stale.md")
	if err := os.WriteFile(stalePath, []byte("# stale\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(stale.md): %v", err)
	}

	m := &compose.Project{
		Meta: compose.Meta{Title: "Example"},
		Sets: map[string]compose.Set{
			"common": newHugoTestSet(nil, func(set *compose.Set) {
				set.SetDescription("Shared settings.")
			}),
		},
	}

	generatedDir, err := prepareBuildContentDirWithOptions(siteRoot, m, false)
	if err != nil {
		t.Fatalf("prepareBuildContentDirWithOptions(): %v", err)
	}

	if _, err := os.Stat(stalePath); err != nil {
		t.Fatalf("expected stale file to remain when refresh is disabled, got: %v", err)
	}

	groupContent, err := os.ReadFile(filepath.Join(generatedDir, "sets", "common.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/common.md): %v", err)
	}
	if !strings.Contains(string(groupContent), "Shared settings.") {
		t.Fatalf("expected generated set page content, got:\n%s", string(groupContent))
	}
	if err := os.RemoveAll(generatedDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestPrepareBuildContentDirUsesDocsIndexAsHome(t *testing.T) {
	siteRoot := t.TempDir()
	docsDir := filepath.Join(siteRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(docs): %v", err)
	}
	readmeHome := "# README Home\n"
	if err := os.WriteFile(filepath.Join(siteRoot, "README.md"), []byte(readmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md): %v", err)
	}

	m := &compose.Project{
		Meta: compose.Meta{Title: "Example"},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	homeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.md): %v", err)
	}
	if string(homeContent) != readmeHome {
		t.Fatalf("expected README.md to be used for home page, got:\n%s", string(homeContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestPrepareBuildContentDirUsesReadmeAsHome(t *testing.T) {
	siteRoot := t.TempDir()
	readmeHome := "# README Home\n"
	if err := os.WriteFile(filepath.Join(siteRoot, "README.md"), []byte(readmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md): %v", err)
	}

	m := &compose.Project{
		Meta: compose.Meta{Title: "Example"},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	homeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.md): %v", err)
	}
	if string(homeContent) != readmeHome {
		t.Fatalf("expected README.md to be used as home page, got:\n%s", string(homeContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestPrepareBuildContentDirUsesLocalizedReadmeAsHome(t *testing.T) {
	siteRoot := t.TempDir()
	readmeHome := "# README Home\n"
	localizedReadmeHome := "# README Startseite\n"
	if err := os.WriteFile(filepath.Join(siteRoot, "README.md"), []byte(readmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteRoot, "README.de.md"), []byte(localizedReadmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.de.md): %v", err)
	}

	m := &compose.Project{
		Meta: compose.Meta{
			Title:               "Example",
			HugoDefaultLanguage: "en",
			HugoLanguages:       "en:\n  languageName: English\n  weight: 1\nde:\n  languageName: Deutsch\n  weight: 2\n",
		},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	homeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.md): %v", err)
	}
	if string(homeContent) != readmeHome {
		t.Fatalf("expected README.md to be used as default home page, got:\n%s", string(homeContent))
	}

	localizedHomeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.de.md): %v", err)
	}
	if string(localizedHomeContent) != localizedReadmeHome {
		t.Fatalf("expected README.de.md to be used as german home page, got:\n%s", string(localizedHomeContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestPrepareBuildContentDirFallsBackToDefaultReadmeForLocalizedHome(t *testing.T) {
	siteRoot := t.TempDir()
	readmeHome := "# README Home\n"
	if err := os.WriteFile(filepath.Join(siteRoot, "README.md"), []byte(readmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md): %v", err)
	}

	m := &compose.Project{
		Meta: compose.Meta{
			Title:               "Example",
			HugoDefaultLanguage: "en",
			HugoLanguages:       "en:\n  languageName: English\n  weight: 1\nde:\n  languageName: Deutsch\n  weight: 2\n",
		},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	localizedHomeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.de.md): %v", err)
	}
	if string(localizedHomeContent) != readmeHome {
		t.Fatalf("expected README.md fallback for german home page, got:\n%s", string(localizedHomeContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestUsesGeneratedHugoSite(t *testing.T) {
	tests := []struct {
		subcommand string
		want       bool
	}{
		{subcommand: "build", want: true},
		{subcommand: "server", want: true},
		{subcommand: "deploy", want: true},
		{subcommand: "version", want: false},
	}

	for _, tt := range tests {
		if got := usesGeneratedHugoSite(tt.subcommand); got != tt.want {
			t.Fatalf("usesGeneratedHugoSite(%q) = %v, want %v", tt.subcommand, got, tt.want)
		}
	}
}

func TestSyncBuildAssetsPreservesEmbeddedIcons(t *testing.T) {
	siteRoot := t.TempDir()
	manifestPath := filepath.Join(siteRoot, "compose.yml")
	manifest := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"services:",
		"  web:",
		"    image: ghcr.io/front-matter/invenio-rdm-starter:latest",
	}, "\n")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yml): %v", err)
	}

	siteDir, err := prepareBuildAssets(manifestPath, false, "")
	if err != nil {
		t.Fatalf("prepareBuildAssets(): %v", err)
	}
	defer os.RemoveAll(siteDir)

	iconsPath := filepath.Join(siteDir, "data", "icons.yaml")
	if _, err := os.Stat(iconsPath); err != nil {
		t.Fatalf("expected embedded icons to exist before sync, err=%v", err)
	}

	if err := syncBuildAssets(manifestPath, siteDir, false, ""); err != nil {
		t.Fatalf("syncBuildAssets(): %v", err)
	}

	if _, err := os.Stat(iconsPath); err != nil {
		t.Fatalf("expected embedded icons to remain after sync, err=%v", err)
	}
}

func TestWriteTempHugoConfigFromManifestIncludesMetaIgnoreLogs(t *testing.T) {
	siteDir := t.TempDir()
	m := &compose.Project{
		Meta: compose.Meta{
			Title:                    "Legacy Example",
			IgnoreLogs:               []string{"legacy-ignore"},
			HugoTitle:                "Hugo Example",
			HugoParamsDescription:    "Configured via HUGO_PARAMS_DESCRIPTION",
			HugoIgnoreLogs:           []string{"warning-goldmark-raw-html"},
			MarkupGoldmarkUnsafe:     "false",
			HugoMarkupGoldmarkUnsafe: "true",
		},
	}

	if err := writeTempHugoConfigFromManifest(m, siteDir, "https://github.com/front-matter/envy", ""); err != nil {
		t.Fatalf("writeTempHugoConfigFromManifest(): %v", err)
	}

	configBytes, err := os.ReadFile(filepath.Join(siteDir, "hugo.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(hugo.yaml): %v", err)
	}

	var got map[string]interface{}
	if err := yaml.Unmarshal(configBytes, &got); err != nil {
		t.Fatalf("yaml.Unmarshal(hugo.yaml): %v", err)
	}

	ignoreLogs, ok := got["ignoreLogs"].([]interface{})
	if !ok || len(ignoreLogs) != 1 || ignoreLogs[0] != "warning-goldmark-raw-html" {
		t.Fatalf("expected ignoreLogs [warning-goldmark-raw-html], got: %#v", got["ignoreLogs"])
	}

	if got["title"] != "Hugo Example" {
		t.Fatalf("expected title Hugo Example, got: %#v", got["title"])
	}

	paramsConfig, ok := got["params"].(map[string]interface{})
	if !ok || paramsConfig["description"] != "Configured via HUGO_PARAMS_DESCRIPTION" {
		t.Fatalf("expected params.description from HUGO_PARAMS_DESCRIPTION, got: %#v", got["params"])
	}

	markup, ok := got["markup"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected markup map, got: %#v", got["markup"])
	}
	goldmark, ok := markup["goldmark"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected markup.goldmark map, got: %#v", markup["goldmark"])
	}
	renderer, ok := goldmark["renderer"].(map[string]interface{})
	if !ok || renderer["unsafe"] != true {
		t.Fatalf("expected markup.goldmark.renderer.unsafe=true, got: %#v", goldmark["renderer"])
	}

	module, ok := got["module"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected module map in hugo config, got: %#v", got["module"])
	}
	imports, ok := module["imports"].([]interface{})
	if !ok || len(imports) == 0 {
		t.Fatalf("expected non-empty module.imports, got: %#v", module["imports"])
	}

	security, ok := got["security"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected security map in hugo config, got: %#v", got["security"])
	}
	if security["enableInlineShortcodes"] != true {
		t.Fatalf("expected security.enableInlineShortcodes=true, got: %#v", security["enableInlineShortcodes"])
	}

	menu, ok := got["menu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected menu map in hugo config, got: %#v", got["menu"])
	}
	mainMenu, ok := menu["main"].([]interface{})
	if !ok || len(mainMenu) < 5 {
		t.Fatalf("expected non-empty menu.main, got: %#v", menu["main"])
	}

	firstMenuItem, ok := mainMenu[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first menu item to be map, got: %#v", mainMenu[0])
	}
	if firstMenuItem["name"] != "Hugo Example" {
		t.Fatalf("expected first config menu item to be the site title, got: %#v", firstMenuItem["name"])
	}
	if firstMenuItem["pageRef"] != "/" {
		t.Fatalf("expected first menu item pageRef to be /, got: %#v", firstMenuItem["pageRef"])
	}

	secondMenuItem, ok := mainMenu[1].(map[string]interface{})
	if !ok {
		t.Fatalf("expected second menu item to be map, got: %#v", mainMenu[1])
	}
	if secondMenuItem["name"] != "Profiles" {
		t.Fatalf("expected second config menu item to be Profiles, got: %#v", secondMenuItem["name"])
	}
	if secondMenuItem["pageRef"] != "/profiles" {
		t.Fatalf("expected second menu item pageRef to be /profiles, got: %#v", secondMenuItem["pageRef"])
	}

	thirdMenuItem, ok := mainMenu[2].(map[string]interface{})
	if !ok {
		t.Fatalf("expected third menu item to be map, got: %#v", mainMenu[2])
	}
	if thirdMenuItem["name"] != "Search" {
		t.Fatalf("expected third config menu item to be Search, got: %#v", thirdMenuItem["name"])
	}
	params, ok := thirdMenuItem["params"].(map[string]interface{})
	if !ok || params["type"] != "search" {
		t.Fatalf("expected third menu item params.type to be search, got: %#v", thirdMenuItem["params"])
	}

	fourthMenuItem, ok := mainMenu[3].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fourth menu item to be map, got: %#v", mainMenu[3])
	}
	if fourthMenuItem["name"] != "Theme" {
		t.Fatalf("expected fourth config menu item to be Theme, got: %#v", fourthMenuItem["name"])
	}
	themeParams, ok := fourthMenuItem["params"].(map[string]interface{})
	if !ok || themeParams["type"] != "theme-toggle" {
		t.Fatalf("expected fourth menu item params.type to be theme-toggle, got: %#v", fourthMenuItem["params"])
	}

	fifthMenuItem, ok := mainMenu[4].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fifth menu item to be map, got: %#v", mainMenu[4])
	}
	if fifthMenuItem["name"] != "GitHub" {
		t.Fatalf("expected fifth config menu item to be GitHub, got: %#v", fifthMenuItem["name"])
	}
	if fifthMenuItem["url"] != "https://github.com/front-matter/envy" {
		t.Fatalf("expected fifth menu item url to be repo URL, got: %#v", fifthMenuItem["url"])
	}
	githubParams, ok := fifthMenuItem["params"].(map[string]interface{})
	if !ok || githubParams["icon"] != "github" {
		t.Fatalf("expected fifth menu item params.icon to be github, got: %#v", fifthMenuItem["params"])
	}
}

func TestWriteTempHugoConfigFromManifestIncludesProfilesMenuWhenProfilesExist(t *testing.T) {
	siteDir := t.TempDir()
	m := &compose.Project{
		Services: []compose.Service{{
			Name:     "web",
			Profiles: []string{"public"},
		}},
	}

	if err := writeTempHugoConfigFromManifest(m, siteDir, "", ""); err != nil {
		t.Fatalf("writeTempHugoConfigFromManifest(): %v", err)
	}

	configBytes, err := os.ReadFile(filepath.Join(siteDir, "hugo.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(hugo.yaml): %v", err)
	}

	var got map[string]interface{}
	if err := yaml.Unmarshal(configBytes, &got); err != nil {
		t.Fatalf("yaml.Unmarshal(hugo.yaml): %v", err)
	}

	menu, ok := got["menu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected menu map in hugo config, got: %#v", got["menu"])
	}
	mainMenu, ok := menu["main"].([]interface{})
	if !ok || len(mainMenu) == 0 {
		t.Fatalf("expected non-empty menu.main, got: %#v", menu["main"])
	}

	foundProfiles := false
	for _, item := range mainMenu {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if entry["name"] == "Profiles" && entry["pageRef"] == "/profiles" {
			foundProfiles = true
			break
		}
	}
	if !foundProfiles {
		t.Fatalf("expected Profiles menu item with pageRef /profiles, got: %#v", menu["main"])
	}
}

func TestWriteTempHugoConfigFromManifestAlwaysIncludesProfilesMenu(t *testing.T) {
	siteDir := t.TempDir()
	m := &compose.Project{}

	if err := writeTempHugoConfigFromManifest(m, siteDir, "", ""); err != nil {
		t.Fatalf("writeTempHugoConfigFromManifest(): %v", err)
	}

	configBytes, err := os.ReadFile(filepath.Join(siteDir, "hugo.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(hugo.yaml): %v", err)
	}

	var got map[string]interface{}
	if err := yaml.Unmarshal(configBytes, &got); err != nil {
		t.Fatalf("yaml.Unmarshal(hugo.yaml): %v", err)
	}

	menu, ok := got["menu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected menu map in hugo config, got: %#v", got["menu"])
	}
	mainMenu, ok := menu["main"].([]interface{})
	if !ok || len(mainMenu) == 0 {
		t.Fatalf("expected non-empty menu.main, got: %#v", menu["main"])
	}

	firstMenuItem, ok := mainMenu[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first menu item to be map, got: %#v", mainMenu[0])
	}
	if firstMenuItem["name"] != "Profiles" || firstMenuItem["pageRef"] != "/profiles" {
		t.Fatalf("expected first menu item to be Profiles with pageRef /profiles, got: %#v", firstMenuItem)
	}
}

func TestPrepareBuildContentDirGeneratesProfilesIndexWhenNoProfilesExist(t *testing.T) {
	siteRoot := t.TempDir()
	m := &compose.Project{
		Meta: compose.Meta{
			HugoDefaultLanguage: "en",
			HugoLanguages:       "en:\n  languageName: English\n  weight: 1\nde:\n  languageName: Deutsch\n  weight: 2\n",
		},
		Services: []compose.Service{{
			Name:  "web",
			Image: "caddy:2.10",
		}},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	profilesIndexContent, err := os.ReadFile(filepath.Join(contentDir, "profiles", "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(profiles/_index.md): %v", err)
	}
	if !strings.Contains(string(profilesIndexContent), `title="none"`) || !strings.Contains(string(profilesIndexContent), `link="/profiles/none/"`) {
		t.Fatalf("expected generated profiles index to include none card, got:\n%s", string(profilesIndexContent))
	}
	if !strings.Contains(string(profilesIndexContent), `cardType="profile"`) {
		t.Fatalf("expected generated profiles index cards to use profile shortcode icon, got:\n%s", string(profilesIndexContent))
	}

	noneProfileContent, err := os.ReadFile(filepath.Join(contentDir, "profiles", "none.md"))
	if err != nil {
		t.Fatalf("ReadFile(profiles/none.md): %v", err)
	}
	if !strings.Contains(string(noneProfileContent), `titleLink="/services/web"`) {
		t.Fatalf("expected none profile page to list services without profiles, got:\n%s", string(noneProfileContent))
	}

	localizedProfilesIndexContent, err := os.ReadFile(filepath.Join(contentDir, "profiles", "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(profiles/_index.de.md): %v", err)
	}
	if !strings.Contains(string(localizedProfilesIndexContent), `title="none"`) || !strings.Contains(string(localizedProfilesIndexContent), `link="/profiles/none/"`) {
		t.Fatalf("expected localized generated profiles index to include none card, got:\n%s", string(localizedProfilesIndexContent))
	}
	if !strings.Contains(string(localizedProfilesIndexContent), `cardType="profile"`) {
		t.Fatalf("expected localized generated profiles index cards to use profile shortcode icon, got:\n%s", string(localizedProfilesIndexContent))
	}
}

func TestPrepareBuildContentDirRefreshesExistingPersistentContent(t *testing.T) {
	siteRoot := t.TempDir()
	contentDir := filepath.Join(siteRoot, persistentContentDirName)
	if err := os.MkdirAll(filepath.Join(contentDir, "profiles"), 0o755); err != nil {
		t.Fatalf("MkdirAll(profiles): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(contentDir, ".envy-generated", "profiles"), 0o755); err != nil {
		t.Fatalf("MkdirAll(marker dir): %v", err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "legacy.md"), []byte("legacy"), 0o644); err != nil {
		t.Fatalf("WriteFile(legacy.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "legacy.md.envy-generated"), []byte("marker"), 0o644); err != nil {
		t.Fatalf("WriteFile(legacy marker): %v", err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "profiles", "stale.md"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile(stale.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, ".envy-generated", "profiles", "stale.md.marker"), []byte("marker"), 0o644); err != nil {
		t.Fatalf("WriteFile(hidden marker): %v", err)
	}

	m := &compose.Project{}
	resultDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}
	if resultDir != contentDir {
		t.Fatalf("expected contentDir %q, got %q", contentDir, resultDir)
	}
	if _, err := os.Stat(filepath.Join(contentDir, "legacy.md")); !os.IsNotExist(err) {
		t.Fatalf("expected stale file to be removed, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(contentDir, "legacy.md.envy-generated")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy marker file to be removed with full refresh, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(contentDir, "profiles", "stale.md")); !os.IsNotExist(err) {
		t.Fatalf("expected stale nested file to be removed, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(contentDir, ".envy-generated", "profiles", "stale.md.marker")); !os.IsNotExist(err) {
		t.Fatalf("expected hidden marker directory to be removed with full refresh, got: %v", err)
	}
}

func TestNormalizeRepositoryURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "https", in: "https://github.com/front-matter/envy.git", want: "https://github.com/front-matter/envy"},
		{name: "http", in: "http://github.com/front-matter/envy.git", want: "http://github.com/front-matter/envy"},
		{name: "scp ssh", in: "git@github.com:front-matter/envy.git", want: "https://github.com/front-matter/envy"},
		{name: "ssh url", in: "ssh://git@github.com/front-matter/envy.git", want: "https://github.com/front-matter/envy"},
		{name: "unsupported", in: "file:///tmp/repo", want: ""},
		{name: "empty", in: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeRepositoryURL(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeRepositoryURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestWriteTempHugoConfigFromManifestIncludesMultilanguage(t *testing.T) {
	siteDir := t.TempDir()
	m := &compose.Project{
		Meta: compose.Meta{
			Title:               "Example",
			HugoDefaultLanguage: "en",
			HugoDefaultInSubdir: "true",
			HugoLanguages:       "en:\n  languageName: English\n  weight: 1\nde:\n  languageName: Deutsch\n  weight: 2\n",
		},
	}

	if err := writeTempHugoConfigFromManifest(m, siteDir, "", ""); err != nil {
		t.Fatalf("writeTempHugoConfigFromManifest(): %v", err)
	}

	configBytes, err := os.ReadFile(filepath.Join(siteDir, "hugo.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(hugo.yaml): %v", err)
	}

	var got map[string]interface{}
	if err := yaml.Unmarshal(configBytes, &got); err != nil {
		t.Fatalf("yaml.Unmarshal(hugo.yaml): %v", err)
	}

	if got["defaultContentLanguage"] != "en" {
		t.Fatalf("expected defaultContentLanguage en, got: %#v", got["defaultContentLanguage"])
	}
	if got["defaultContentLanguageInSubdir"] != true {
		t.Fatalf("expected defaultContentLanguageInSubdir true, got: %#v", got["defaultContentLanguageInSubdir"])
	}

	languages, ok := got["languages"].(map[string]interface{})
	if !ok || len(languages) != 2 {
		t.Fatalf("expected languages map with 2 entries, got: %#v", got["languages"])
	}
	if _, ok := languages["en"]; !ok {
		t.Fatalf("expected languages.en to exist, got: %#v", languages)
	}
	if _, ok := languages["de"]; !ok {
		t.Fatalf("expected languages.de to exist, got: %#v", languages)
	}

	menu, ok := got["menu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected menu map in hugo config, got: %#v", got["menu"])
	}
	mainMenu, ok := menu["main"].([]interface{})
	if !ok || len(mainMenu) < 3 {
		t.Fatalf("expected menu.main with language switch, got: %#v", menu["main"])
	}

	foundLanguageSwitch := false
	for _, item := range mainMenu {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		params, ok := entry["params"].(map[string]interface{})
		if !ok {
			continue
		}
		if params["type"] == "language-switch" {
			foundLanguageSwitch = true
			break
		}
	}
	if !foundLanguageSwitch {
		t.Fatalf("expected language-switch menu item in menu.main, got: %#v", menu["main"])
	}

	deConfig, ok := languages["de"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected languages.de to be map, got: %#v", languages["de"])
	}
	deMenu, ok := deConfig["menu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected languages.de.menu map, got: %#v", deConfig["menu"])
	}
	deMainMenu, ok := deMenu["main"].([]interface{})
	if !ok || len(deMainMenu) < 5 {
		t.Fatalf("expected localized languages.de.menu.main, got: %#v", deMenu["main"])
	}

	deFirstMenuItem, ok := deMainMenu[0].(map[string]interface{})
	if !ok || deFirstMenuItem["name"] != "Example" {
		t.Fatalf("expected first german menu item to be the site title, got: %#v", deMainMenu[0])
	}
	deSecondMenuItem, ok := deMainMenu[1].(map[string]interface{})
	if !ok || deSecondMenuItem["name"] != "Profile" {
		t.Fatalf("expected second german menu item to be Profile, got: %#v", deMainMenu[1])
	}
	deThirdMenuItem, ok := deMainMenu[2].(map[string]interface{})
	if !ok || deThirdMenuItem["name"] != "Suche" {
		t.Fatalf("expected third german menu item to be Suche, got: %#v", deMainMenu[2])
	}
	deFourthMenuItem, ok := deMainMenu[3].(map[string]interface{})
	if !ok || deFourthMenuItem["name"] != "Design" {
		t.Fatalf("expected fourth german menu item to be Design, got: %#v", deMainMenu[3])
	}
	deFifthMenuItem, ok := deMainMenu[4].(map[string]interface{})
	if !ok || deFifthMenuItem["name"] != "Sprache" {
		t.Fatalf("expected fifth german menu item to be Sprache, got: %#v", deMainMenu[4])
	}
}
