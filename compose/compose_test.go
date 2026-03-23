package compose

import (
	"strings"
	"testing"

	types "github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"
)

func newTestSet(vars types.MappingWithEquals, configure ...func(*Set)) Set {
	set := NewSet()
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

func TestStringDefaultUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "bool true", yaml: "default: true\n", want: "true"},
		{name: "bool false", yaml: "default: false\n", want: "false"},
		{name: "int", yaml: "default: 5\n", want: "5"},
		{name: "string", yaml: "default: hello\n", want: "hello"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var data struct {
				Default string `yaml:"default"`
			}
			if err := yaml.Unmarshal([]byte(test.yaml), &data); err != nil {
				t.Fatalf("yaml.Unmarshal() error = %v", err)
			}
			if got := data.Default; got != test.want {
				t.Fatalf("default = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDecodeSetParsesComposeInterpolationSyntax(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-application: &application",
		"  INVENIO_ACCOUNTS_LOCAL_LOGIN_ENABLED: ${INVENIO_ACCOUNTS_LOCAL_LOGIN_ENABLED:-true}",
		"  INVENIO_RDM_SITE_NAME: ${INVENIO_RDM_SITE_NAME:?required}",
		"  INVENIO_INSTANCE_PATH: /opt/invenio/var/instance",
		"  INVENIO_HOSTNAME: ${INVENIO_HOSTNAME}",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	set, ok := m.Sets["application"]
	if !ok {
		t.Fatalf("expected application set")
	}

	if got := VarString(set.Vars()["INVENIO_ACCOUNTS_LOCAL_LOGIN_ENABLED"]); got != "true" {
		t.Fatalf("unexpected parsed login var: %q", got)
	}
	if got := VarString(set.Vars()["INVENIO_RDM_SITE_NAME"]); got != "required" {
		t.Fatalf("unexpected parsed required var: %q", got)
	}
	if got := VarString(set.Vars()["INVENIO_HOSTNAME"]); got != "" {
		t.Fatalf("unexpected parsed bare interpolation var: %q", got)
	}
	if got := VarString(set.Vars()["INVENIO_INSTANCE_PATH"]); got != "/opt/invenio/var/instance" {
		t.Fatalf("unexpected parsed literal var: %q", got)
	}
}

func TestManifestMarshalOmitsEmptyFields(t *testing.T) {
	m := Project{
		Meta: Meta{
			Title:   "Imported Compose Project",
			Version: "v1",
		},
		Services: []Service{{
			Name:  "web",
			Image: "ghcr.io/example/web:latest",
		}},
		Sets: map[string]Set{
			"web": newTestSet(types.MappingWithEquals{"APP_ENV": strPtr("")}),
		},
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	output := string(data)
	checks := []string{
		"description: \"\"",
		"docs: \"\"",
		"platform: \"\"",
		"entrypoint: []",
		"command: []",
	}

	for _, check := range checks {
		if strings.Contains(output, check) {
			t.Fatalf("yaml output unexpectedly contains %q\n%s", check, output)
		}
	}

	if !strings.Contains(output, "APP_ENV: \"\"") {
		t.Fatalf("expected empty defaults to be preserved, got:\n%s", output)
	}
}

func TestManifestMarshalKeepsServicesWithoutAssociatedVars(t *testing.T) {
	m := Project{
		Meta: Meta{Title: "Imported Compose Project", Version: "v1"},
		Services: []Service{
			{Name: "web", Sets: []string{"web"}},
			{Name: "cache", Sets: []string{"cache"}},
		},
		Sets: map[string]Set{
			"web":   newTestSet(types.MappingWithEquals{"APP_ENV": nil}),
			"cache": newTestSet(nil),
		},
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "cache: {}") {
		t.Fatalf("expected service without associated vars to be kept, got:\n%s", output)
	}
	if strings.Contains(output, "x-set-cache:") {
		t.Fatalf("expected empty set to be omitted, got:\n%s", output)
	}
}

func TestManifestMarshalBoolLikeDefaultsAsStrings(t *testing.T) {
	m := Project{
		Meta: Meta{Title: "Imported Env Project", Version: "v1"},
		Sets: map[string]Set{
			"env": newTestSet(types.MappingWithEquals{
				"STRING_VALUE": strPtr("production"),
				"BOOL_TRUE":    strPtr("true"),
				"BOOL_FALSE":   strPtr("false"),
			}),
		},
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "BOOL_TRUE: \"true\"") {
		t.Fatalf("expected quoted string default true, got:\n%s", output)
	}
	if !strings.Contains(output, "BOOL_FALSE: \"false\"") {
		t.Fatalf("expected quoted string default false, got:\n%s", output)
	}
	if strings.Contains(output, "BOOL_TRUE: true\n") {
		t.Fatalf("did not expect YAML boolean true, got:\n%s", output)
	}
	if strings.Contains(output, "BOOL_FALSE: false\n") {
		t.Fatalf("did not expect YAML boolean false, got:\n%s", output)
	}
}

func TestManifestMarshalServiceCommandAsFlowList(t *testing.T) {
	m := Project{
		Meta: Meta{Title: "Imported Compose Project", Version: "v1"},
		Services: []Service{{
			Name:    "worker",
			Image:   "ghcr.io/example/worker:latest",
			Command: []string{"celery", "worker"},
			Sets:    []string{"app"},
		}},
		Sets: map[string]Set{
			"app": newTestSet(types.MappingWithEquals{"CELERY_BROKER_URL": nil}),
		},
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "services:\n    worker:") {
		t.Fatalf("expected services to be written as mapping style, got:\n%s", output)
	}
	if !strings.Contains(output, "command: [\"celery\", \"worker\"]") {
		t.Fatalf("expected command in flow-list format, got:\n%s", output)
	}
	if !strings.Contains(output, "x-set-app: &app") {
		t.Fatalf("expected app set to be written as anchored x-set key, got:\n%s", output)
	}
	if !strings.Contains(output, "environment:") || !strings.Contains(output, "<<: [*app]") {
		t.Fatalf("expected service set refs to be written as environment merge aliases, got:\n%s", output)
	}
	if strings.Contains(output, "command:\n") {
		t.Fatalf("did not expect block-list command format, got:\n%s", output)
	}
}

func TestManifestMarshalVolumesAsComposeStyleMap(t *testing.T) {
	m := Project{Meta: Meta{Title: "Imported Compose Project", Version: "v1"}}
	m.SetVolumeNames([]string{"app_data", "uploaded_data"})

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "volumes:\n    app_data:\n    uploaded_data:\n") {
		t.Fatalf("expected volumes to be written as compose-style mapping, got:\n%s", output)
	}
	if strings.Contains(output, "volumes:\n    - app_data") {
		t.Fatalf("did not expect list-style volumes, got:\n%s", output)
	}
}

func TestManifestLoadVolumesFromComposeStyleMap(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"  version: v1",
		"volumes:",
		"  app_data:",
		"  uploaded_data:",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	volumes := m.VolumeNames()
	if len(volumes) != 2 || volumes[0] != "app_data" || volumes[1] != "uploaded_data" {
		t.Fatalf("expected volumes [app_data uploaded_data], got %+v", volumes)
	}
}

func TestManifestLoadServicesAndVars(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"  version: v1",
		"x-set-application: &application",
		"  description: App settings",
		"  APP_ENV:",
		"    default: production",
		"services:",
		"  web:",
		"    image: ghcr.io/example/web:latest",
		"    environment:",
		"      <<: [*application]",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if len(m.Services) != 1 || m.Services[0].Name != "web" {
		t.Fatalf("expected one service named web, got %+v", m.Services)
	}
	if len(m.Services[0].Sets) != 1 || m.Services[0].Sets[0] != "application" {
		t.Fatalf("expected service set application, got %+v", m.Services[0].Sets)
	}

	set, ok := m.Sets["application"]
	if !ok {
		t.Fatalf("expected application set")
	}
	if len(set.Vars()) != 1 {
		t.Fatalf("expected APP_ENV var, got %+v", set.Vars())
	}
	if got := VarString(set.Vars()["APP_ENV"]); got != "production" {
		t.Fatalf("expected default production, got %q", got)
	}
}

func TestManifestLoadServiceScalarSet(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"  version: v1",
		"x-set-coolify: &coolify",
		"  description: Coolify settings",
		"services:",
		"  web:",
		"    image: ghcr.io/example/web:latest",
		"    environment:",
		"      <<: *coolify",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if len(m.Services) != 1 {
		t.Fatalf("expected one service, got %+v", m.Services)
	}
	if len(m.Services[0].Sets) != 1 || m.Services[0].Sets[0] != "coolify" {
		t.Fatalf("expected scalar sets value to normalize to [coolify], got %+v", m.Services[0].Sets)
	}
}

func TestManifestLoadServiceDescriptionFromComments(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"services:",
		"  # Describes db service configuration.",
		"  db:",
		"    image: postgres:17.4-bookworm",
		"    environment:",
		"      POSTGRES_DB: app",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if len(m.Services) != 1 {
		t.Fatalf("expected one service, got %+v", m.Services)
	}
	if m.Services[0].Description != "Describes db service configuration." {
		t.Fatalf("expected service description from comment, got %q", m.Services[0].Description)
	}
}

func TestManifestLoadServiceDescriptionFromInterEntryComments(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"services:",
		"  cache:",
		"    image: valkey/valkey:7.2.5-bookworm",
		"  # Describes db service configuration.",
		"  # Additional line.",
		"  db:",
		"    image: postgres:17.4-bookworm",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if len(m.Services) != 2 {
		t.Fatalf("expected two services, got %+v", m.Services)
	}
	if m.Services[1].Name != "db" {
		t.Fatalf("expected second service db, got %q", m.Services[1].Name)
	}
	if m.Services[1].Description != "Describes db service configuration. Additional line." {
		t.Fatalf("expected multi-line service description from comments, got %q", m.Services[1].Description)
	}
}

func TestManifestLoadServiceDescriptionFromCommentsWithStandaloneLink(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"services:",
		"  # Describes search service configuration. For details see",
		"  # https://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/",
		"  search:",
		"    image: opensearchproject/opensearch:2.18.0",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	want := "Describes search service configuration. For details see\nhttps://docs.opensearch.org/latest/install-and-configure/install-opensearch/docker/"
	if m.Services[0].Description != want {
		t.Fatalf("expected service description with standalone link preserved, got %q", m.Services[0].Description)
	}
}

func TestManifestLoadIgnoresLegacySecretField(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"  version: v1",
		"x-set-app: &app",
		"  SECRET_KEY:",
		"    default: super-secret",
		"    secret: true",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	set, ok := m.Sets["app"]
	if !ok || len(set.Vars()) != 1 {
		t.Fatalf("expected app set with one var, got %#v", m.Sets)
	}
	if got := VarString(set.Vars()["SECRET_KEY"]); got != "super-secret" {
		t.Fatalf("expected legacy secret field to be ignored and default preserved, got %q", got)
	}
}

func TestManifestLoadInlineSetVarWithEmptyInterpolation(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-app: &app",
		"  INVENIO_OIDC_ISSUER: \"${INVENIO_OIDC_ISSUER:-}\"",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if got := VarString(m.Sets["app"].Vars()["INVENIO_OIDC_ISSUER"]); got != "" {
		t.Fatalf("expected empty default, got %q", got)
	}
}

func TestManifestLoadInlineSetVarWithBareEmptyValue(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-app: &app",
		"  INVENIO_OIDC_ISSUER:",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if got := VarString(m.Sets["app"].Vars()["INVENIO_OIDC_ISSUER"]); got != "" {
		t.Fatalf("expected empty default to stay empty, got %q", got)
	}
}

func TestManifestLoadInlineSetVarWithExplicitEmptyString(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-app: &app",
		"  INVENIO_OIDC_ISSUER: \"\"",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if got := VarString(m.Sets["app"].Vars()["INVENIO_OIDC_ISSUER"]); got != "" {
		t.Fatalf("expected empty default, got %q", got)
	}
}

func TestManifestLoadGroupLink(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-common: &common",
		"  description: Shared settings",
		"  link: https://example.org/common",
		"  APP_ENV:",
		"    default: production",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	set, ok := m.Sets["common"]
	if !ok {
		t.Fatalf("expected common set")
	}
	if set.Link() != "https://example.org/common" {
		t.Fatalf("expected set link to be preserved, got %q", set.Link())
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), "link: https://example.org/common") {
		t.Fatalf("expected marshaled YAML to contain set link, got:\n%s", string(data))
	}
}

func TestManifestLoadSetDescriptionAndLinkFromComments(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"# Shared environment variables for web and worker.",
		"# link: https://example.org/base",
		"x-set-base: &base",
		"  APP_ENV:",
		"    default: production",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	set, ok := m.Sets["base"]
	if !ok {
		t.Fatalf("expected base set")
	}
	if set.Description() != "Shared environment variables for web and worker." {
		t.Fatalf("expected set description from comment, got %q", set.Description())
	}
	if set.Link() != "https://example.org/base" {
		t.Fatalf("expected set link from comment, got %q", set.Link())
	}
}

func TestManifestLoadSetLinkFromMarkdownComment(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"# Shared settings",
		"# [Docs](https://example.org/docs)",
		"x-set-common: &common",
		"  APP_ENV:",
		"    default: production",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	set := m.Sets["common"]
	if set.Description() != "Shared settings" {
		t.Fatalf("expected set description from comment, got %q", set.Description())
	}
	if set.Link() != "https://example.org/docs" {
		t.Fatalf("expected markdown link extraction, got %q", set.Link())
	}
}

func TestManifestLoadSetDescriptionAndLinkFromInterEntryComments(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-app: &app",
		"  APP_ENV:",
		"    default: production",
		"# Worker-only settings",
		"# link: https://example.org/worker",
		"x-set-worker: &worker",
		"  WORKER_CONCURRENCY:",
		"    default: \"4\"",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	set := m.Sets["worker"]
	if set.Description() != "Worker-only settings" {
		t.Fatalf("expected set description from inter-entry comments, got %q", set.Description())
	}
	if set.Link() != "https://example.org/worker" {
		t.Fatalf("expected set link from inter-entry comments, got %q", set.Link())
	}
}

func TestParseSetMetadataFromCommentsIgnoresSlashSlashLines(t *testing.T) {
	description, link := parseSetMetadataFromComments(strings.Join([]string{
		"// This line should be ignored",
		"# Shared settings",
		"// link: https://example.org/ignored",
		"# link: https://example.org/base",
	}, "\n"))

	if description != "Shared settings" {
		t.Fatalf("expected description from # comment, got %q", description)
	}
	if link != "https://example.org/base" {
		t.Fatalf("expected link from # comment, got %q", link)
	}
}

func TestDecodeVarsSequenceFormat(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-app: &app",
		"  vars:",
		"    - key: DATABASE_URL",
		"      default: postgres://localhost/app",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if got := VarString(m.Sets["app"].Vars()["DATABASE_URL"]); got != "postgres://localhost/app" {
		t.Fatalf("expected DATABASE_URL default, got %q", got)
	}
}

func TestDecodeVarsMappingFormat(t *testing.T) {
	input := strings.Join([]string{
		"x-envy:",
		"  title: Example",
		"x-set-app: &app",
		"  vars:",
		"    CACHE_URL:",
		"      default: redis://localhost:6379",
	}, "\n")

	var m Project
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if got := VarString(m.Sets["app"].Vars()["CACHE_URL"]); got != "redis://localhost:6379" {
		t.Fatalf("expected CACHE_URL default, got %q", got)
	}
}

func TestAllVarsReturnsMergedMap(t *testing.T) {
	m := &Project{
		Sets: map[string]Set{
			"base": newTestSet(types.MappingWithEquals{"APP_ENV": strPtr("production")}),
			"web":  newTestSet(types.MappingWithEquals{"PORT": strPtr("8080")}),
		},
	}

	all := m.AllVars()
	if len(all) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(all))
	}
	if got := VarString(all["APP_ENV"]); got != "production" {
		t.Fatalf("expected APP_ENV=production, got %q", got)
	}
	if got := VarString(all["PORT"]); got != "8080" {
		t.Fatalf("expected PORT=8080, got %q", got)
	}
}

func TestVarsForServiceReturnsMergedSetVars(t *testing.T) {
	m := &Project{
		Services: []Service{{Name: "web", Sets: []string{"base", "web"}}},
		Sets: map[string]Set{
			"base": newTestSet(types.MappingWithEquals{"APP_ENV": strPtr("production")}),
			"web":  newTestSet(types.MappingWithEquals{"PORT": strPtr("8080")}),
		},
	}

	vars := m.VarsForService("web")
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if got := VarString(vars["APP_ENV"]); got != "production" {
		t.Fatalf("expected APP_ENV=production, got %q", got)
	}
	if got := VarString(vars["PORT"]); got != "8080" {
		t.Fatalf("expected PORT=8080, got %q", got)
	}
}

func TestIsValidPlatform(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{name: "os arch", value: "linux/amd64", expected: true},
		{name: "os arch variant", value: "linux/arm64/v8", expected: true},
		{name: "missing arch", value: "linux", expected: false},
		{name: "too many parts", value: "linux/arm64/v8/extra", expected: false},
		{name: "empty part", value: "linux//v8", expected: false},
		{name: "space in part", value: "linux/arm 64", expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := isValidPlatform(test.value); actual != test.expected {
				t.Fatalf("isValidPlatform(%q) = %v, want %v", test.value, actual, test.expected)
			}
		})
	}
}

func TestIsValidImageReference(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{name: "simple image", value: "alpine:latest", expected: true},
		{name: "registry namespace tag", value: "ghcr.io/front-matter/invenio-rdm-starter:latest", expected: true},
		{name: "registry port and digest", value: "registry.example.com:5000/team/app@sha256:abcdef0123456789", expected: true},
		{name: "url scheme", value: "https://ghcr.io/front-matter/app:latest", expected: false},
		{name: "space", value: "ghcr.io/front matter/app:latest", expected: false},
		{name: "uppercase repository", value: "ghcr.io/Front-Matter/app:latest", expected: false},
		{name: "empty", value: "", expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := isValidImageReference(test.value); actual != test.expected {
				t.Fatalf("isValidImageReference(%q) = %v, want %v", test.value, actual, test.expected)
			}
		})
	}
}

func TestLintWarnsForInvalidServiceImageAndPlatform(t *testing.T) {
	m := &Project{
		Meta: Meta{Title: "Example"},
		Services: []Service{{
			Name:     "web",
			Image:    "https://ghcr.io/front-matter/app:latest",
			Platform: "linux",
			Sets:     []string{"application"},
		}},
		Sets: map[string]Set{"application": {}},
	}

	issues := m.LintIssues()
	joined := strings.Join(m.Lint(), "\n")

	if !strings.Contains(joined, "service-image-require-explicit-tag") || !strings.Contains(joined, "invalid image") {
		t.Fatalf("expected invalid image lint issue, got %q", joined)
	}

	foundPlatform := false
	for _, issue := range issues {
		if issue.Rule == "service-platform-format" {
			foundPlatform = true
			break
		}
	}
	if !foundPlatform {
		t.Fatalf("expected service-platform-format issue, got %#v", issues)
	}
}

func TestLintRejectsLatestTag(t *testing.T) {
	m := &Project{
		Meta: Meta{Title: "Example"},
		Services: []Service{{
			Name:  "web",
			Image: "ghcr.io/front-matter/invenio-rdm-starter:latest",
			Sets:  []string{"application"},
		}},
		Sets: map[string]Set{"application": {}},
	}

	issues := m.LintIssues()
	found := false
	for _, issue := range issues {
		if issue.Rule == "service-image-require-explicit-tag" && issue.Level == "warning" && strings.Contains(issue.Message, "unstable tag") {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected unstable-tag lint issue, got %#v", issues)
	}
}

func TestLintErrorsWhenSetIsUnusedByServices(t *testing.T) {
	m := &Project{
		Meta: Meta{Title: "Example"},
		Services: []Service{{
			Name:  "web",
			Image: "ghcr.io/front-matter/invenio-rdm-starter:v1.2.3",
			Sets:  []string{"app"},
		}},
		Sets: map[string]Set{
			"app":    {},
			"unused": {},
		},
	}

	issues := m.LintIssues()
	found := false
	for _, issue := range issues {
		if issue.Rule == "x-set-anchor-must-be-used" && issue.Level == "error" && issue.Path == "x-set-unused" {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected unused set lint issue, got %#v", issues)
	}
}

func TestLintDoesNotErrorWhenAllSetsAreUsed(t *testing.T) {
	m := &Project{
		Meta: Meta{Title: "Example"},
		Services: []Service{{
			Name:  "web",
			Image: "ghcr.io/front-matter/invenio-rdm-starter:v1.2.3",
			Sets:  []string{"app", "shared"},
		}},
		Sets: map[string]Set{
			"app":    {},
			"shared": {},
		},
	}

	for _, issue := range m.LintIssues() {
		if issue.Rule == "x-set-anchor-must-be-used" {
			t.Fatalf("unexpected unused set lint issue: %#v", issue)
		}
	}
}
