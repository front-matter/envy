package manifest

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

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

func TestManifestMarshalOmitsEmptyFields(t *testing.T) {
	m := Manifest{
		Meta: Meta{
			Title:   "Imported Compose Manifest",
			Version: "v1",
		},
		Services: []Service{{
			Name:  "web",
			Image: "ghcr.io/example/web:latest",
		}},
		Sets: map[string]Set{
			"web": {
				Vars: []Var{{
					Key: "APP_ENV",
				}},
			},
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
		"allowed: []",
		"example: \"\"",
	}

	for _, check := range checks {
		if strings.Contains(output, check) {
			t.Fatalf("yaml output unexpectedly contains %q\n%s", check, output)
		}
	}

	if !strings.Contains(output, "default: \"\"") {
		t.Fatalf("expected empty defaults to be preserved, got:\n%s", output)
	}
}

func TestManifestMarshalKeepsServicesWithoutAssociatedVars(t *testing.T) {
	m := Manifest{
		Meta: Meta{Title: "Imported Compose Manifest", Version: "v1"},
		Services: []Service{
			{Name: "web", Sets: []string{"web"}},
			{Name: "cache", Sets: []string{"cache"}},
		},
		Sets: map[string]Set{
			"web": {
				Vars: []Var{{Key: "APP_ENV"}},
			},
			"cache": {
				Vars: nil,
			},
		},
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "\n    cache:\n") {
		t.Fatalf("expected service without associated vars to be kept, got:\n%s", output)
	}
	if strings.Contains(output, "sets:\n    cache:") {
		t.Fatalf("expected empty set to be omitted, got:\n%s", output)
	}
}

func TestManifestMarshalBoolLikeDefaultsAsStrings(t *testing.T) {
	m := Manifest{
		Meta: Meta{Title: "Imported Env Manifest", Version: "v1"},
		Sets: map[string]Set{
			"env": {
				Vars: []Var{
					{Key: "STRING_VALUE", Default: "production", Example: "demo-value", Required: "true", Secret: "true"},
					{Key: "BOOL_TRUE", Default: "true"},
					{Key: "BOOL_FALSE", Default: "false"},
				},
			},
		},
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "default: \"true\"") {
		t.Fatalf("expected quoted string default true, got:\n%s", output)
	}
	if !strings.Contains(output, "default: \"false\"") {
		t.Fatalf("expected quoted string default false, got:\n%s", output)
	}
	if !strings.Contains(output, "default: \"\"") {
		t.Fatalf("expected quoted empty default for secret var, got:\n%s", output)
	}
	if !strings.Contains(output, "example: \"demo-value\"") {
		t.Fatalf("expected quoted string example value, got:\n%s", output)
	}
	if !strings.Contains(output, "required: \"true\"") {
		t.Fatalf("expected quoted string required true, got:\n%s", output)
	}
	if !strings.Contains(output, "secret: \"true\"") {
		t.Fatalf("expected quoted string secret true, got:\n%s", output)
	}
	if strings.Contains(output, "default: true\n") {
		t.Fatalf("did not expect YAML boolean true, got:\n%s", output)
	}
	if strings.Contains(output, "default: false\n") {
		t.Fatalf("did not expect YAML boolean false, got:\n%s", output)
	}
	if strings.Contains(output, "required: true\n") {
		t.Fatalf("did not expect YAML boolean required true, got:\n%s", output)
	}
	if strings.Contains(output, "secret: true\n") {
		t.Fatalf("did not expect YAML boolean secret true, got:\n%s", output)
	}
	if strings.Contains(output, "editable: true\n") {
		t.Fatalf("did not expect YAML boolean editable true, got:\n%s", output)
	}
	if !strings.Contains(output, "BOOL_TRUE:") {
		t.Fatalf("expected set vars to be written as mapping style, got:\n%s", output)
	}
}

func TestManifestMarshalServiceCommandAsFlowList(t *testing.T) {
	m := Manifest{
		Meta: Meta{Title: "Imported Compose Manifest", Version: "v1"},
		Services: []Service{{
			Name:    "worker",
			Image:   "ghcr.io/example/worker:latest",
			Command: []string{"celery", "worker"},
			Sets:    []string{"app"},
		}},
		Sets: map[string]Set{
			"app": {Vars: []Var{{Key: "CELERY_BROKER_URL"}}},
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
	if !strings.Contains(output, "sets: [app]") {
		t.Fatalf("expected service sets to be written as inline list, got:\n%s", output)
	}
	if strings.Contains(output, "command:\n") {
		t.Fatalf("did not expect block-list command format, got:\n%s", output)
	}
}

func TestManifestMarshalVolumesAsComposeStyleMap(t *testing.T) {
	m := Manifest{
		Meta:    Meta{Title: "Imported Compose Manifest", Version: "v1"},
		Volumes: []string{"app_data", "uploaded_data"},
	}

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
		"meta:",
		"  title: Example",
		"  version: v1",
		"volumes:",
		"  app_data:",
		"  uploaded_data:",
	}, "\n")

	var m Manifest
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if len(m.Volumes) != 2 || m.Volumes[0] != "app_data" || m.Volumes[1] != "uploaded_data" {
		t.Fatalf("expected volumes [app_data uploaded_data], got %+v", m.Volumes)
	}
}

func TestManifestLoadServicesAndVars(t *testing.T) {
	input := strings.Join([]string{
		"meta:",
		"  title: Example",
		"  version: v1",
		"services:",
		"  web:",
		"    image: ghcr.io/example/web:latest",
		"    sets: [application]",
		"sets:",
		"  application:",
		"    description: App settings",
		"    vars:",
		"      APP_ENV:",
		"        default: production",
		"        required: true",
	}, "\n")

	var m Manifest
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
	if len(set.Vars) != 1 || set.Vars[0].Key != "APP_ENV" {
		t.Fatalf("expected APP_ENV var, got %+v", set.Vars)
	}
	if set.Vars[0].Default != "production" {
		t.Fatalf("expected default production, got %q", set.Vars[0].Default)
	}
}

func TestManifestLoadServiceScalarSet(t *testing.T) {
	input := strings.Join([]string{
		"meta:",
		"  title: Example",
		"  version: v1",
		"services:",
		"  web:",
		"    image: ghcr.io/example/web:latest",
		"    sets: coolify",
		"sets:",
		"  coolify:",
		"    description: Coolify settings",
	}, "\n")

	var m Manifest
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

func TestManifestLoadSecretDefaultIsAlwaysEmpty(t *testing.T) {
	input := strings.Join([]string{
		"meta:",
		"  title: Example",
		"  version: v1",
		"sets:",
		"  app:",
		"    vars:",
		"      SECRET_KEY:",
		"        default: super-secret",
		"        secret: true",
	}, "\n")

	var m Manifest
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	set, ok := m.Sets["app"]
	if !ok || len(set.Vars) != 1 {
		t.Fatalf("expected app set with one var, got %#v", m.Sets)
	}

	secret := set.Vars[0]
	if !secret.IsSecret() {
		t.Fatalf("expected var to be secret")
	}
	if secret.Default != "" {
		t.Fatalf("expected secret default to be empty, got %q", secret.Default)
	}
	if secret.DefaultString() != "" {
		t.Fatalf("expected secret DefaultString to be empty, got %q", secret.DefaultString())
	}
}

func TestManifestLoadGroupLink(t *testing.T) {
	input := strings.Join([]string{
		"meta:",
		"  title: Example",
		"sets:",
		"  common:",
		"    description: Shared settings",
		"    link: https://example.org/common",
		"    vars:",
		"      APP_ENV:",
		"        default: production",
	}, "\n")

	var m Manifest
	if err := yaml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	set, ok := m.Sets["common"]
	if !ok {
		t.Fatalf("expected common set")
	}
	if set.Link != "https://example.org/common" {
		t.Fatalf("expected set link to be preserved, got %q", set.Link)
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), "link: https://example.org/common") {
		t.Fatalf("expected marshaled YAML to contain set link, got:\n%s", string(data))
	}
}

func TestManifestMarshalOmitsImportedComposeVarDescription(t *testing.T) {
	m := Manifest{
		Meta: Meta{Title: "Imported Compose Manifest", Version: "v1"},
		Sets: map[string]Set{
			"web": {
				Vars: []Var{{
					Key:         "APP_ENV",
					Description: "Imported from compose environment",
				}},
			},
		},
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	output := string(data)
	if strings.Contains(output, "description: Imported from compose environment") {
		t.Fatalf("expected imported compose var description to be omitted, got:\n%s", output)
	}
}

func TestManifestMarshalOmitsImportedEnvFileVarDescription(t *testing.T) {
	m := Manifest{
		Meta: Meta{Title: "Imported Env Manifest", Version: "v1"},
		Sets: map[string]Set{
			"env": {
				Vars: []Var{{
					Key:         "APP_ENV",
					Description: "Imported from .env file",
				}},
			},
		},
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	output := string(data)
	if strings.Contains(output, "description: Imported from .env file") {
		t.Fatalf("expected imported .env var description to be omitted, got:\n%s", output)
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
	m := &Manifest{
		Services: []Service{
			{
				Name:     "web",
				Image:    "https://ghcr.io/front-matter/app:latest",
				Platform: "linux",
				Sets:     []string{"application"},
			},
		},
		Sets: map[string]Set{
			"application": {},
		},
	}

	warnings := strings.Join(m.Lint(), "\n")

	if !strings.Contains(warnings, "services.web: invalid image") {
		t.Fatalf("expected invalid image warning, got %q", warnings)
	}

	if !strings.Contains(warnings, "services.web: invalid platform") {
		t.Fatalf("expected invalid platform warning, got %q", warnings)
	}
}

func TestLintAllowsMissingPlatform(t *testing.T) {
	m := &Manifest{
		Services: []Service{
			{
				Name:  "web",
				Image: "ghcr.io/front-matter/invenio-rdm-starter:latest",
				Sets:  []string{"application"},
			},
		},
		Sets: map[string]Set{
			"application": {},
		},
	}

	for _, warning := range m.Lint() {
		if strings.Contains(warning, "platform") {
			t.Fatalf("unexpected platform warning: %q", warning)
		}
	}
}
