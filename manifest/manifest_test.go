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
			Name:    "Imported Compose Manifest",
			Version: "v1",
		},
		Services: []Service{{
			Name:  "web",
			Image: "ghcr.io/example/web:latest",
		}},
		Groups: map[string]Group{
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
		Meta: Meta{Name: "Imported Compose Manifest", Version: "v1"},
		Services: []Service{
			{Name: "web", Groups: []string{"web"}},
			{Name: "cache", Groups: []string{"cache"}},
		},
		Groups: map[string]Group{
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
	if strings.Contains(output, "groups:\n    cache:") {
		t.Fatalf("expected empty group to be omitted, got:\n%s", output)
	}
}

func TestManifestMarshalBoolLikeDefaultsAsStrings(t *testing.T) {
	m := Manifest{
		Meta: Meta{Name: "Imported Env Manifest", Version: "v1"},
		Groups: map[string]Group{
			"env": {
				Vars: []Var{
					{Key: "STRING_VALUE", Default: "production", Example: "demo-value"},
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
	if !strings.Contains(output, "default: \"production\"") {
		t.Fatalf("expected quoted string default production, got:\n%s", output)
	}
	if !strings.Contains(output, "example: \"demo-value\"") {
		t.Fatalf("expected quoted string example value, got:\n%s", output)
	}
	if strings.Contains(output, "default: true\n") {
		t.Fatalf("did not expect YAML boolean true, got:\n%s", output)
	}
	if strings.Contains(output, "default: false\n") {
		t.Fatalf("did not expect YAML boolean false, got:\n%s", output)
	}
	if !strings.Contains(output, "BOOL_TRUE:") {
		t.Fatalf("expected group vars to be written as mapping style, got:\n%s", output)
	}
}

func TestManifestMarshalServiceCommandAsFlowList(t *testing.T) {
	m := Manifest{
		Meta: Meta{Name: "Imported Compose Manifest", Version: "v1"},
		Services: []Service{{
			Name:    "worker",
			Image:   "ghcr.io/example/worker:latest",
			Command: []string{"celery", "worker"},
			Groups:  []string{"app"},
		}},
		Groups: map[string]Group{
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
	if !strings.Contains(output, "groups: [app]") {
		t.Fatalf("expected service groups to be written as inline list, got:\n%s", output)
	}
	if strings.Contains(output, "command:\n") {
		t.Fatalf("did not expect block-list command format, got:\n%s", output)
	}
}

func TestManifestMarshalVolumesAsComposeStyleMap(t *testing.T) {
	m := Manifest{
		Meta:    Meta{Name: "Imported Compose Manifest", Version: "v1"},
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
		"  name: Example",
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
		"  name: Example",
		"  version: v1",
		"services:",
		"  web:",
		"    image: ghcr.io/example/web:latest",
		"    groups: [application]",
		"groups:",
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
	if len(m.Services[0].Groups) != 1 || m.Services[0].Groups[0] != "application" {
		t.Fatalf("expected service group application, got %+v", m.Services[0].Groups)
	}

	group, ok := m.Groups["application"]
	if !ok {
		t.Fatalf("expected application group")
	}
	if len(group.Vars) != 1 || group.Vars[0].Key != "APP_ENV" {
		t.Fatalf("expected APP_ENV var, got %+v", group.Vars)
	}
	if group.Vars[0].Default != "production" {
		t.Fatalf("expected default production, got %q", group.Vars[0].Default)
	}
}

func TestManifestMarshalOmitsImportedComposeVarDescription(t *testing.T) {
	m := Manifest{
		Meta: Meta{Name: "Imported Compose Manifest", Version: "v1"},
		Groups: map[string]Group{
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
		Meta: Meta{Name: "Imported Env Manifest", Version: "v1"},
		Groups: map[string]Group{
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
				Groups:   []string{"application"},
			},
		},
		Groups: map[string]Group{
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
				Name:   "web",
				Image:  "ghcr.io/front-matter/invenio-rdm-starter:latest",
				Groups: []string{"application"},
			},
		},
		Groups: map[string]Group{
			"application": {},
		},
	}

	for _, warning := range m.Lint() {
		if strings.Contains(warning, "platform") {
			t.Fatalf("unexpected platform warning: %q", warning)
		}
	}
}
