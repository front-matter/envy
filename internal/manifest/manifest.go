// Package manifest loads and provides access to the env.yaml spec.
package manifest

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest is the top-level env.yaml structure.
type Manifest struct {
	Meta   Meta    `yaml:"meta"`
	Groups []Group `yaml:"groups"`
}

// Meta holds project-level metadata.
type Meta struct {
	Name           string `yaml:"name"`
	Description    string `yaml:"description"`
	InvenioVersion string `yaml:"invenio_version"`
	Docs           string `yaml:"docs"`
}

// Group is a logical grouping of related variables.
type Group struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Vars        []Var  `yaml:"vars"`
}

// Var defines a single environment variable's spec.
type Var struct {
	Key         string   `yaml:"key"`
	Default     string   `yaml:"default"`
	Description string   `yaml:"description"`
	Required    bool     `yaml:"required"`
	Secret      bool     `yaml:"secret"`
	Type        string   `yaml:"type"` // string | bool | int | url | python_literal
	Allowed     []string `yaml:"allowed"`
	MinLength   int      `yaml:"min_length"`
	Example     string   `yaml:"example"`
}

// AllVars returns a flat slice of all variables across all groups.
func (m *Manifest) AllVars() []Var {
	var vars []Var
	for _, g := range m.Groups {
		vars = append(vars, g.Vars...)
	}
	return vars
}

// SecretVars returns only variables marked secret.
func (m *Manifest) SecretVars() []Var {
	var vars []Var
	for _, v := range m.AllVars() {
		if v.Secret {
			vars = append(vars, v)
		}
	}
	return vars
}

// RequiredVars returns only variables marked required.
func (m *Manifest) RequiredVars() []Var {
	var vars []Var
	for _, v := range m.AllVars() {
		if v.Required {
			vars = append(vars, v)
		}
	}
	return vars
}

// Lint returns warnings for values that are legal but potentially ambiguous.
func (m *Manifest) Lint() []string {
	var warnings []string
	boolTraps := map[string]bool{
		"yes": true, "no": true, "on": true, "off": true,
		"true": true, "false": true,
	}

	for _, v := range m.AllVars() {
		if boolTraps[strings.ToLower(v.Default)] && v.Type != "bool" {
			warnings = append(warnings, fmt.Sprintf(
				"%s: default %q may be parsed as bool - quote it explicitly",
				v.Key, v.Default,
			))
		}
	}

	return warnings
}

// Load reads and parses an env.yaml file.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", path, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
	}
	return &m, nil
}
