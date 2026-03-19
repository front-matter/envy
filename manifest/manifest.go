// Package manifest loads and provides access to the env.yaml spec.
package manifest

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var dockerImagePattern = regexp.MustCompile(`^([A-Za-z0-9.-]+(?::[0-9]+)?/)?[a-z0-9]+([._-][a-z0-9]+)*(\/[a-z0-9]+([._-][a-z0-9]+)*)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(?:@[A-Za-z][A-Za-z0-9]*:[A-Za-z0-9=_:+.-]+)?$`)

// Manifest is the top-level env.yaml structure.
type Manifest struct {
	Meta     Meta           `yaml:"meta"`
	Services []Service      `yaml:"services,omitempty"`
	Sets     map[string]Set `yaml:"sets,omitempty"`
	Volumes  []string       `yaml:"volumes,omitempty"`
	Networks []string       `yaml:"networks,omitempty"`
}

// MarshalYAML omits sets with no vars and writes services/vars in mapping style.
func (m Manifest) MarshalYAML() (interface{}, error) {
	filteredSets := make(map[string]Set)
	for key, set := range m.Sets {
		if len(set.Vars) > 0 {
			filteredSets[key] = set
		}
	}

	root := &yaml.Node{Kind: yaml.MappingNode}

	metaNode, err := encodeNode(m.Meta)
	if err != nil {
		return nil, err
	}
	appendMapping(root, "meta", metaNode)

	if len(m.Services) > 0 {
		servicesNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, svc := range m.Services {
			serviceNode, err := svc.MarshalYAML()
			if err != nil {
				return nil, err
			}
			encoded, err := encodeNode(serviceNode)
			if err != nil {
				return nil, err
			}
			appendMapping(servicesNode, svc.Name, encoded)
		}
		appendMapping(root, "services", servicesNode)
	}

	if len(filteredSets) > 0 {
		setsNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, set := range m.OrderedSets() {
			filtered, ok := filteredSets[set.Key]
			if !ok {
				continue
			}
			setNode, err := encodeSetNode(filtered)
			if err != nil {
				return nil, err
			}
			appendMapping(setsNode, set.Key, setNode)
		}
		appendMapping(root, "sets", setsNode)
	}

	if len(m.Volumes) > 0 {
		volumesNode := encodeNamedEmptyMapNode(m.Volumes)
		appendMapping(root, "volumes", volumesNode)
	}

	if len(m.Networks) > 0 {
		networksNode, err := encodeStringSliceNode(m.Networks)
		if err != nil {
			return nil, err
		}
		appendMapping(root, "networks", networksNode)
	}

	return root, nil
}

func (m *Manifest) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected manifest mapping, got YAML kind %d", node.Kind)
	}

	var out Manifest
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]

		switch key {
		case "meta":
			if err := value.Decode(&out.Meta); err != nil {
				return err
			}
		case "services":
			services, err := decodeServicesNode(value)
			if err != nil {
				return err
			}
			out.Services = services
		case "sets":
			sets, err := decodeSetsNode(value)
			if err != nil {
				return err
			}
			out.Sets = sets
		case "volumes":
			volumes, err := decodeNamedEntriesNode(value)
			if err != nil {
				return err
			}
			out.Volumes = volumes
		case "networks":
			if err := value.Decode(&out.Networks); err != nil {
				return err
			}
		}
	}

	*m = out
	if m.Sets == nil {
		m.Sets = make(map[string]Set)
	}
	return nil
}

// Meta holds project-level metadata.
type Meta struct {
	Title        string   `yaml:"title,omitempty"`
	Docs         string   `yaml:"docs,omitempty"`
	Author       string   `yaml:"author,omitempty"`
	LanguageCode string   `yaml:"languageCode,omitempty"`
	Description  string   `yaml:"description,omitempty"`
	Version      string   `yaml:"version,omitempty"`
	IgnoreLogs   []string `yaml:"ignoreLogs,omitempty"`
}

// VersionLabel returns the configured version label.
func (m Meta) VersionLabel() string {
	return m.Version
}

// LanguageCodeLabel returns languageCode with en-US default.
func (m Meta) LanguageCodeLabel() string {
	if strings.TrimSpace(m.LanguageCode) == "" {
		return "en-US"
	}
	return m.LanguageCode
}

// Service describes a runtime service and the sets it uses.
type Service struct {
	Name        string   `yaml:"name"`
	Image       string   `yaml:"image,omitempty"`
	Platform    string   `yaml:"platform,omitempty"`
	Entrypoint  []string `yaml:"entrypoint,omitempty"`
	Command     []string `yaml:"command,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Sets        []string `yaml:"sets,omitempty"`
}

type serviceYAML struct {
	Image       string     `yaml:"image,omitempty"`
	Platform    string     `yaml:"platform,omitempty"`
	Entrypoint  []string   `yaml:"entrypoint,omitempty"`
	Command     *yaml.Node `yaml:"command,omitempty"`
	Description string     `yaml:"description,omitempty"`
	Sets        *yaml.Node `yaml:"sets,omitempty"`
}

// MarshalYAML emits command and sets in flow style.
func (s Service) MarshalYAML() (interface{}, error) {
	out := serviceYAML{
		Image:       s.Image,
		Platform:    s.Platform,
		Entrypoint:  s.Entrypoint,
		Description: s.Description,
	}

	if len(s.Command) > 0 {
		node := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, item := range s.Command {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: item})
		}
		out.Command = node
	}

	if len(s.Sets) > 0 {
		node := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, item := range s.Sets {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: item})
		}
		out.Sets = node
	}

	return out, nil
}

func (s *Service) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected service mapping, got YAML kind %d", node.Kind)
	}

	var out Service
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]

		switch key {
		case "image":
			if err := value.Decode(&out.Image); err != nil {
				return err
			}
		case "platform":
			if err := value.Decode(&out.Platform); err != nil {
				return err
			}
		case "entrypoint":
			if err := value.Decode(&out.Entrypoint); err != nil {
				return err
			}
		case "command":
			if err := value.Decode(&out.Command); err != nil {
				return err
			}
		case "description":
			if err := value.Decode(&out.Description); err != nil {
				return err
			}
		case "sets":
			if err := value.Decode(&out.Sets); err != nil {
				return err
			}
		}
	}

	*s = out
	return nil
}

func isValidPlatform(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}

	for _, part := range parts {
		if strings.TrimSpace(part) == "" || strings.Contains(part, " ") {
			return false
		}
	}

	return true
}

func isValidImageReference(value string) bool {
	if strings.TrimSpace(value) == "" || strings.Contains(value, "://") || strings.Contains(value, " ") {
		return false
	}

	return dockerImagePattern.MatchString(value)
}

// Set is a logical grouping of related variables.
type Set struct {
	Key         string `yaml:"-"`
	Description string `yaml:"description,omitempty"`
	Link        string `yaml:"link,omitempty"`
	Vars        []Var  `yaml:"vars,omitempty"`
}

type setYAML struct {
	Description string     `yaml:"description,omitempty"`
	Link        string     `yaml:"link,omitempty"`
	Vars        *yaml.Node `yaml:"vars,omitempty"`
}

// OrderedSets returns all sets in deterministic order.
func (m *Manifest) OrderedSets() []Set {
	keys := make([]string, 0, len(m.Sets))
	for key := range m.Sets {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	sets := make([]Set, 0, len(keys))
	for _, key := range keys {
		set := m.Sets[key]
		set.Key = key
		sets = append(sets, set)
	}

	return sets
}

// Var defines a single environment variable's spec.
type Var struct {
	Key         string `yaml:"key"`
	Description string `yaml:"description,omitempty"`
	Default     string `yaml:"default,omitempty"`
	Required    string `yaml:"required,omitempty"`
	Secret      string `yaml:"secret,omitempty"`
	Editable    string `yaml:"editable,omitempty"`
	Example     string `yaml:"example,omitempty"`
}

func (v Var) DefaultString() string {
	if v.IsSecret() {
		return ""
	}
	return v.Default
}

func (v *Var) UnmarshalYAML(node *yaml.Node) error {
	type rawVar Var
	var decoded rawVar
	if err := node.Decode(&decoded); err != nil {
		return err
	}

	if strings.EqualFold(strings.TrimSpace(decoded.Secret), "true") {
		decoded.Default = ""
	}

	*v = Var(decoded)
	return nil
}

func (v Var) IsRequired() bool {
	return strings.EqualFold(strings.TrimSpace(v.Required), "true")
}

func (v Var) IsSecret() bool {
	return strings.EqualFold(strings.TrimSpace(v.Secret), "true")
}

func (v Var) IsEditable() bool {
	return strings.EqualFold(strings.TrimSpace(v.Editable), "true")
}

// MarshalYAML omits import placeholder descriptions and emits defaults as strings.
func (v Var) MarshalYAML() (interface{}, error) {
	description := v.Description
	if description == "Imported from compose environment" || description == "Imported from .env file" {
		description = ""
	}

	node := &yaml.Node{Kind: yaml.MappingNode}
	if description != "" {
		appendMapping(node, "description", &yaml.Node{Kind: yaml.ScalarNode, Value: description})
	}
	appendMapping(node, "default", &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: v.DefaultString()})
	if v.IsRequired() {
		appendMapping(node, "required", &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: "true"})
	}
	if v.IsSecret() {
		appendMapping(node, "secret", &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: "true"})
	}
	if v.IsEditable() {
		appendMapping(node, "editable", &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: "true"})
	}
	if v.Example != "" {
		appendMapping(node, "example", &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: v.Example})
	}

	return node, nil
}

func encodeNode(value interface{}) (*yaml.Node, error) {
	var doc yaml.Node
	if err := doc.Encode(value); err != nil {
		return nil, err
	}
	if len(doc.Content) == 1 {
		return doc.Content[0], nil
	}
	return &doc, nil
}

func encodeStringSliceNode(values []string) (*yaml.Node, error) {
	return encodeNode(values)
}

func encodeNamedEmptyMapNode(values []string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, value := range values {
		appendMapping(node, value, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: ""})
	}
	return node
}

func appendMapping(node *yaml.Node, key string, value *yaml.Node) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		value,
	)
}

func encodeSetNode(set Set) (*yaml.Node, error) {
	setNode := &yaml.Node{Kind: yaml.MappingNode}
	if set.Description != "" {
		appendMapping(setNode, "description", &yaml.Node{Kind: yaml.ScalarNode, Value: set.Description})
	}
	if set.Link != "" {
		appendMapping(setNode, "link", &yaml.Node{Kind: yaml.ScalarNode, Value: set.Link})
	}
	if len(set.Vars) > 0 {
		varsNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, v := range set.Vars {
			varNodeValue, err := v.MarshalYAML()
			if err != nil {
				return nil, err
			}
			encoded, err := encodeNode(varNodeValue)
			if err != nil {
				return nil, err
			}
			if encoded.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("expected var mapping, got YAML kind %d", encoded.Kind)
			}
			appendMapping(varsNode, v.Key, encoded)
		}
		appendMapping(setNode, "vars", varsNode)
	}
	return setNode, nil
}

func decodeServicesNode(node *yaml.Node) ([]Service, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected services mapping, got YAML kind %d", node.Kind)
	}

	services := make([]Service, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		name := node.Content[i].Value
		var svc Service
		if err := node.Content[i+1].Decode(&svc); err != nil {
			return nil, err
		}
		svc.Name = name
		services = append(services, svc)
	}
	return services, nil
}

func decodeSetsNode(node *yaml.Node) (map[string]Set, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected sets mapping, got YAML kind %d", node.Kind)
	}

	sets := make(map[string]Set, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		set, err := decodeSetNode(node.Content[i+1])
		if err != nil {
			return nil, err
		}
		sets[key] = set
	}
	return sets, nil
}

func decodeSetNode(node *yaml.Node) (Set, error) {
	var out Set
	if node.Kind != yaml.MappingNode {
		return out, fmt.Errorf("expected set mapping, got YAML kind %d", node.Kind)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]
		switch key {
		case "description":
			if err := value.Decode(&out.Description); err != nil {
				return out, err
			}
		case "link":
			if err := value.Decode(&out.Link); err != nil {
				return out, err
			}
		case "vars":
			vars, err := decodeVarsNode(value)
			if err != nil {
				return out, err
			}
			out.Vars = vars
		}
	}
	return out, nil
}

func decodeVarsNode(node *yaml.Node) ([]Var, error) {
	switch node.Kind {
	case yaml.SequenceNode:
		vars := make([]Var, 0, len(node.Content))
		for _, item := range node.Content {
			var v Var
			if err := item.Decode(&v); err != nil {
				return nil, err
			}
			vars = append(vars, v)
		}
		return vars, nil
	case yaml.MappingNode:
		vars := make([]Var, 0, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			var v Var
			if err := node.Content[i+1].Decode(&v); err != nil {
				return nil, err
			}
			v.Key = key
			vars = append(vars, v)
		}
		return vars, nil
	default:
		return nil, fmt.Errorf("expected vars sequence or mapping, got YAML kind %d", node.Kind)
	}
}

func decodeNamedEntriesNode(node *yaml.Node) ([]string, error) {
	switch node.Kind {
	case yaml.SequenceNode:
		entries := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			var value string
			if err := item.Decode(&value); err != nil {
				return nil, err
			}
			entries = append(entries, value)
		}
		return entries, nil
	case yaml.MappingNode:
		entries := make([]string, 0, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			entries = append(entries, node.Content[i].Value)
		}
		return entries, nil
	default:
		return nil, fmt.Errorf("expected sequence or mapping, got YAML kind %d", node.Kind)
	}
}

// AllVars returns a flat slice of all variables across all sets.
func (m *Manifest) AllVars() []Var {
	var vars []Var
	for _, set := range m.OrderedSets() {
		vars = append(vars, set.Vars...)
	}
	return vars
}

// SetVars holds vars for a single set.
type SetVars struct {
	SetKey      string
	Description string
	Vars        []Var
}

// VarsForServiceBySet returns vars per set for a service, preserving set order.
func (m *Manifest) VarsForServiceBySet(serviceName string) []SetVars {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		var result []SetVars
		for _, set := range m.OrderedSets() {
			result = append(result, SetVars{SetKey: set.Key, Description: set.Description, Vars: set.Vars})
		}
		return result
	}

	for _, svc := range m.Services {
		if svc.Name != name {
			continue
		}
		var result []SetVars
		for _, setKey := range svc.Sets {
			set, ok := m.Sets[setKey]
			if !ok {
				continue
			}
			result = append(result, SetVars{SetKey: setKey, Description: set.Description, Vars: set.Vars})
		}
		return result
	}

	var result []SetVars
	for _, set := range m.OrderedSets() {
		result = append(result, SetVars{SetKey: set.Key, Description: set.Description, Vars: set.Vars})
	}
	return result
}

// VarsForService returns vars for one service, or all vars if service is unknown.
func (m *Manifest) VarsForService(serviceName string) []Var {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return m.AllVars()
	}

	for _, svc := range m.Services {
		if svc.Name != name {
			continue
		}

		var vars []Var
		for _, setKey := range svc.Sets {
			set, ok := m.Sets[setKey]
			if !ok {
				continue
			}
			vars = append(vars, set.Vars...)
		}
		return vars
	}

	return m.AllVars()
}

// SecretVars returns only variables marked secret.
func (m *Manifest) SecretVars() []Var {
	var vars []Var
	for _, v := range m.AllVars() {
		if v.IsSecret() {
			vars = append(vars, v)
		}
	}
	return vars
}

// RequiredVars returns only variables marked required.
func (m *Manifest) RequiredVars() []Var {
	var vars []Var
	for _, v := range m.AllVars() {
		if v.IsRequired() {
			vars = append(vars, v)
		}
	}
	return vars
}

// Lint returns warnings for values that are legal but potentially ambiguous.
func (m *Manifest) Lint() []string {
	var warnings []string
	seenServices := make(map[string]bool)

	for _, svc := range m.Services {
		if strings.TrimSpace(svc.Name) == "" {
			warnings = append(warnings, "services: found service with empty name")
			continue
		}

		if seenServices[svc.Name] {
			warnings = append(warnings, fmt.Sprintf(
				"services.%s: duplicate service name",
				svc.Name,
			))
		}
		seenServices[svc.Name] = true

		if strings.TrimSpace(svc.Image) == "" {
			warnings = append(warnings, fmt.Sprintf(
				"services.%s: no image configured",
				svc.Name,
			))
		} else if !isValidImageReference(strings.TrimSpace(svc.Image)) {
			warnings = append(warnings, fmt.Sprintf(
				"services.%s: invalid image %q - expected docker image reference like ghcr.io/org/app:tag",
				svc.Name, svc.Image,
			))
		}

		platform := strings.TrimSpace(svc.Platform)
		if platform != "" && !isValidPlatform(platform) {
			warnings = append(warnings, fmt.Sprintf(
				"services.%s: invalid platform %q - expected os/arch or os/arch/variant",
				svc.Name, platform,
			))
		}

		for _, setKey := range svc.Sets {
			if _, ok := m.Sets[setKey]; !ok {
				warnings = append(warnings, fmt.Sprintf(
					"services.%s: unknown set %q",
					svc.Name, setKey,
				))
			}
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
