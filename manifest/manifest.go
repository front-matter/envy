// Package manifest loads and provides access to the env.yaml spec.
package manifest

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var dockerImagePattern = regexp.MustCompile(`^([A-Za-z0-9.-]+(?::[0-9]+)?/)?[a-z0-9]+([._-][a-z0-9]+)*(\/[a-z0-9]+([._-][a-z0-9]+)*)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(?:@[A-Za-z][A-Za-z0-9]*:[A-Za-z0-9=_:+.-]+)?$`)

// Manifest is the top-level env.yaml structure.
type Manifest struct {
	Meta     Meta             `yaml:"meta"`
	Services []Service        `yaml:"services,omitempty"`
	Groups   map[string]Group `yaml:"groups,omitempty"`
	Volumes  []string         `yaml:"volumes,omitempty"`
	Networks []string         `yaml:"networks,omitempty"`
}

// MarshalYAML omits groups with no vars and writes services/vars in mapping style.
func (m Manifest) MarshalYAML() (interface{}, error) {
	filteredGroups := make(map[string]Group)
	for key, group := range m.Groups {
		if len(group.Vars) > 0 {
			filteredGroups[key] = group
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

	if len(filteredGroups) > 0 {
		groupsNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, group := range m.OrderedGroups() {
			filtered, ok := filteredGroups[group.Key]
			if !ok {
				continue
			}
			groupNode, err := encodeGroupNode(filtered)
			if err != nil {
				return nil, err
			}
			appendMapping(groupsNode, group.Key, groupNode)
		}
		appendMapping(root, "groups", groupsNode)
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
		case "groups":
			groups, err := decodeGroupsNode(value)
			if err != nil {
				return err
			}
			out.Groups = groups
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
	if m.Groups == nil {
		m.Groups = make(map[string]Group)
	}
	return nil
}

// Meta holds project-level metadata.
type Meta struct {
	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
	Version     string `yaml:"version,omitempty"`
	Docs        string `yaml:"docs,omitempty"`
}

// VersionLabel returns the configured version label.
func (m Meta) VersionLabel() string {
	return m.Version
}

// Service describes a runtime service and the groups it uses.
type Service struct {
	Name        string   `yaml:"name"`
	Image       string   `yaml:"image,omitempty"`
	Platform    string   `yaml:"platform,omitempty"`
	Entrypoint  []string `yaml:"entrypoint,omitempty"`
	Command     []string `yaml:"command,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Groups      []string `yaml:"groups,omitempty"`
}

type serviceYAML struct {
	Image       string     `yaml:"image,omitempty"`
	Platform    string     `yaml:"platform,omitempty"`
	Entrypoint  []string   `yaml:"entrypoint,omitempty"`
	Command     *yaml.Node `yaml:"command,omitempty"`
	Description string     `yaml:"description,omitempty"`
	Groups      *yaml.Node `yaml:"groups,omitempty"`
}

// MarshalYAML emits command and groups in flow style.
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

	if len(s.Groups) > 0 {
		node := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, item := range s.Groups {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: item})
		}
		out.Groups = node
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
		case "groups":
			if err := value.Decode(&out.Groups); err != nil {
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

// Group is a logical grouping of related variables.
type Group struct {
	Key         string `yaml:"-"`
	Description string `yaml:"description,omitempty"`
	Vars        []Var  `yaml:"vars,omitempty"`
}

type groupYAML struct {
	Description string     `yaml:"description,omitempty"`
	Vars        *yaml.Node `yaml:"vars,omitempty"`
}

// ScalarValue stores a YAML scalar default and exposes it as a string for env generation.
type ScalarValue string

func (v *ScalarValue) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == 0 {
		*v = ""
		return nil
	}

	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("expected scalar default, got YAML kind %d", node.Kind)
	}

	switch node.Tag {
	case "!!bool":
		parsed, err := strconv.ParseBool(node.Value)
		if err != nil {
			return err
		}
		*v = ScalarValue(strconv.FormatBool(parsed))
	case "!!int", "!!float", "!!str", "!!null", "":
		*v = ScalarValue(node.Value)
	default:
		*v = ScalarValue(node.Value)
	}

	return nil
}

func (v ScalarValue) String() string {
	return string(v)
}

// OrderedGroups returns all groups in deterministic order.
func (m *Manifest) OrderedGroups() []Group {
	keys := make([]string, 0, len(m.Groups))
	for key := range m.Groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	groups := make([]Group, 0, len(keys))
	for _, key := range keys {
		g := m.Groups[key]
		g.Key = key
		groups = append(groups, g)
	}

	return groups
}

// Var defines a single environment variable's spec.
type Var struct {
	Key         string      `yaml:"key"`
	Description string      `yaml:"description,omitempty"`
	Default     ScalarValue `yaml:"default,omitempty"`
	Required    bool        `yaml:"required,omitempty"`
	Secret      bool        `yaml:"secret,omitempty"`
	Example     string      `yaml:"example,omitempty"`
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
	appendMapping(node, "default", &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: v.Default.String()})
	if v.Required {
		appendMapping(node, "required", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	if v.Secret {
		appendMapping(node, "secret", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
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

func encodeGroupNode(group Group) (*yaml.Node, error) {
	groupNode := &yaml.Node{Kind: yaml.MappingNode}
	if group.Description != "" {
		appendMapping(groupNode, "description", &yaml.Node{Kind: yaml.ScalarNode, Value: group.Description})
	}
	if len(group.Vars) > 0 {
		varsNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, v := range group.Vars {
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
		appendMapping(groupNode, "vars", varsNode)
	}
	return groupNode, nil
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

func decodeGroupsNode(node *yaml.Node) (map[string]Group, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected groups mapping, got YAML kind %d", node.Kind)
	}

	groups := make(map[string]Group, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		group, err := decodeGroupNode(node.Content[i+1])
		if err != nil {
			return nil, err
		}
		groups[key] = group
	}
	return groups, nil
}

func decodeGroupNode(node *yaml.Node) (Group, error) {
	var out Group
	if node.Kind != yaml.MappingNode {
		return out, fmt.Errorf("expected group mapping, got YAML kind %d", node.Kind)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]
		switch key {
		case "description":
			if err := value.Decode(&out.Description); err != nil {
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

// AllVars returns a flat slice of all variables across all groups.
func (m *Manifest) AllVars() []Var {
	var vars []Var
	for _, g := range m.OrderedGroups() {
		vars = append(vars, g.Vars...)
	}
	return vars
}

// GroupVars holds vars for a single group.
type GroupVars struct {
	GroupKey    string
	Description string
	Vars        []Var
}

// VarsForServiceByGroup returns vars per group for a service, preserving group order.
func (m *Manifest) VarsForServiceByGroup(serviceName string) []GroupVars {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		var result []GroupVars
		for _, g := range m.OrderedGroups() {
			result = append(result, GroupVars{GroupKey: g.Key, Description: g.Description, Vars: g.Vars})
		}
		return result
	}

	for _, svc := range m.Services {
		if svc.Name != name {
			continue
		}
		var result []GroupVars
		for _, groupKey := range svc.Groups {
			group, ok := m.Groups[groupKey]
			if !ok {
				continue
			}
			result = append(result, GroupVars{GroupKey: groupKey, Description: group.Description, Vars: group.Vars})
		}
		return result
	}

	var result []GroupVars
	for _, g := range m.OrderedGroups() {
		result = append(result, GroupVars{GroupKey: g.Key, Description: g.Description, Vars: g.Vars})
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
		for _, groupKey := range svc.Groups {
			group, ok := m.Groups[groupKey]
			if !ok {
				continue
			}
			vars = append(vars, group.Vars...)
		}
		return vars
	}

	return m.AllVars()
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

		for _, groupKey := range svc.Groups {
			if _, ok := m.Groups[groupKey]; !ok {
				warnings = append(warnings, fmt.Sprintf(
					"services.%s: unknown group %q",
					svc.Name, groupKey,
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
