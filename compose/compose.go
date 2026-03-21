// Package compose loads and provides access to the compose.yaml spec.
package compose

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	types "github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"
)

var dockerImagePattern = regexp.MustCompile(`^([A-Za-z0-9.-]+(?::[0-9]+)?/)?[a-z0-9]+([._-][a-z0-9]+)*(\/[a-z0-9]+([._-][a-z0-9]+)*)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(?:@[A-Za-z][A-Za-z0-9]*:[A-Za-z0-9=_:+.-]+)?$`)
var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\((https?://[^)\s]+)\)`)
var markdownReferenceLinkPattern = regexp.MustCompile(`^\[[^\]]+\]:\s*(https?://\S+)$`)
var plainLinkPattern = regexp.MustCompile(`^https?://\S+$`)
var prefixedLinkPattern = regexp.MustCompile(`(?i)^link:\s*(https?://\S+)$`)
var composeInterpolationPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-?])(.*))?\}$`)

// Project is the top-level structure used by envy. It embeds compose-go's Project
// as the canonical type and adds envy-specific metadata and functionality.
type Project struct {
	*types.Project
	Meta     Meta           `yaml:"x-envy"`
	Services Services       `yaml:"services"`
	Sets     map[string]Set `yaml:"-"`
}

// Meta holds envy-specific metadata.
type Meta struct {
	Title                    string   `yaml:"title,omitempty"`
	Docs                     string   `yaml:"docs,omitempty"`
	Author                   string   `yaml:"author,omitempty"`
	LanguageCode             string   `yaml:"languageCode,omitempty"`
	Description              string   `yaml:"description,omitempty"`
	Version                  string   `yaml:"version,omitempty"`
	IgnoreLogs               []string `yaml:"ignoreLogs,omitempty"`
	MarkupGoldmarkUnsafe     string   `yaml:"markupGoldmarkUnsafe,omitempty"`
	HugoTitle                string   `yaml:"HUGO_TITLE,omitempty"`
	HugoParamsDescription    string   `yaml:"HUGO_PARAMS_DESCRIPTION,omitempty"`
	HugoIgnoreLogs           []string `yaml:"HUGO_IGNORE_LOGS,omitempty"`
	HugoMarkupGoldmarkUnsafe string   `yaml:"HUGO_MARKUP_GOLDMARK_UNSAFE,omitempty"`
	HugoLanguageCode         string   `yaml:"HUGO_LANGUAGE_CODE,omitempty"`
	HugoDefaultLanguage      string   `yaml:"HUGO_DEFAULT_CONTENT_LANGUAGE,omitempty"`
	HugoDefaultInSubdir      string   `yaml:"HUGO_DEFAULT_CONTENT_LANGUAGE_IN_SUBDIR,omitempty"`
	HugoLanguages            string   `yaml:"HUGO_LANGUAGES,omitempty"`
}

// Services describes the ordered list of runtime services.
type Services []Service

// Service describes a runtime service and the sets it uses.
type Service struct {
	Name        string   `yaml:"name"`
	Image       string   `yaml:"image,omitempty"`
	Platform    string   `yaml:"platform,omitempty"`
	Entrypoint  []string `yaml:"entrypoint,omitempty"`
	Command     []string `yaml:"command,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Sets        []string `yaml:"-"`
}

type serviceYAML struct {
	Image       string     `yaml:"image,omitempty"`
	Platform    string     `yaml:"platform,omitempty"`
	Entrypoint  []string   `yaml:"entrypoint,omitempty"`
	Command     *yaml.Node `yaml:"command,omitempty"`
	Description string     `yaml:"description,omitempty"`
	Sets        *yaml.Node `yaml:"sets,omitempty"`
}

// Set is a logical grouping of related env variables.
type Set struct {
	Key         string `yaml:"-"`
	Description string `yaml:"description,omitempty"`
	Link        string `yaml:"link,omitempty"`
	Vars        []Var  `yaml:"vars,omitempty"`
}

// Var defines a single environment variable's spec.
type Var struct {
	Key         string `yaml:"key"`
	Description string `yaml:"description,omitempty"`
	Default     string `yaml:"default,omitempty"`
	Required    string `yaml:"required,omitempty"`
	Secret      string `yaml:"secret,omitempty"`
	Readonly    string `yaml:"readonly,omitempty"`
	Example     string `yaml:"example,omitempty"`
}

// SetVars holds vars for a single set.
type SetVars struct {
	SetKey      string
	Description string
	Vars        []Var
}

// LintIssue represents one lint finding.
type LintIssue struct {
	Level   string // error | warning
	Rule    string
	Path    string
	Message string
}

func (p *Project) ensureComposeProject() {
	if p.Project == nil {
		p.Project = &types.Project{}
	}
	if p.Project.Services == nil {
		p.Project.Services = types.Services{}
	}
	if p.Project.Volumes == nil {
		p.Project.Volumes = types.Volumes{}
	}
	if p.Project.Networks == nil {
		p.Project.Networks = types.Networks{}
	}
}

func (p *Project) setVolumeNames(names []string) {
	p.ensureComposeProject()
	p.Project.Volumes = types.Volumes{}
	for _, name := range names {
		p.Project.Volumes[name] = types.VolumeConfig{}
	}
}

// SetVolumeNames replaces project volumes by name.
func (p *Project) SetVolumeNames(names []string) {
	p.setVolumeNames(names)
}

func (p *Project) setNetworkNames(names []string) {
	p.ensureComposeProject()
	p.Project.Networks = types.Networks{}
	for _, name := range names {
		p.Project.Networks[name] = types.NetworkConfig{}
	}
}

// SetNetworkNames replaces project networks by name.
func (p *Project) SetNetworkNames(names []string) {
	p.setNetworkNames(names)
}

func (p *Project) volumeNames() []string {
	if p == nil || p.Project == nil || len(p.Project.Volumes) == 0 {
		return nil
	}
	names := make([]string, 0, len(p.Project.Volumes))
	for name := range p.Project.Volumes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// VolumeNames returns sorted volume names from embedded compose-go project data.
func (p *Project) VolumeNames() []string {
	return p.volumeNames()
}

func (p *Project) networkNames() []string {
	if p == nil || p.Project == nil || len(p.Project.Networks) == 0 {
		return nil
	}
	names := make([]string, 0, len(p.Project.Networks))
	for name := range p.Project.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NetworkNames returns sorted network names from embedded compose-go project data.
func (p *Project) NetworkNames() []string {
	return p.networkNames()
}

// MarshalYAML omits sets with no vars and writes services/vars in mapping style.
func (m Project) MarshalYAML() (interface{}, error) {
	filteredSets := make(map[string]Set)
	for key, set := range m.Sets {
		if len(set.Vars) > 0 {
			filteredSets[key] = set
		}
	}

	root := &yaml.Node{Kind: yaml.MappingNode}
	setNodes := make(map[string]*yaml.Node, len(filteredSets))

	metaNode, err := encodeNode(m.Meta)
	if err != nil {
		return nil, err
	}
	appendMapping(root, "x-envy", metaNode)

	if len(filteredSets) > 0 {
		for _, set := range m.OrderedSets() {
			filtered, ok := filteredSets[set.Key]
			if !ok {
				continue
			}
			setNode, err := encodeSetNode(filtered)
			if err != nil {
				return nil, err
			}
			setNode.Anchor = set.Key
			setNodes[set.Key] = setNode
			appendMapping(root, "x-set-"+set.Key, setNode)
		}
	}

	if len(m.Services) > 0 {
		servicesNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, svc := range m.Services {
			encoded, err := encodeServiceNode(svc, setNodes)
			if err != nil {
				return nil, err
			}
			appendMapping(servicesNode, svc.Name, encoded)
		}
		appendMapping(root, "services", servicesNode)
	}

	volumeNames := m.volumeNames()
	if len(volumeNames) > 0 {
		volumesNode := encodeNamedEmptyMapNode(volumeNames)
		appendMapping(root, "volumes", volumesNode)
	}

	networkNames := m.networkNames()
	if len(networkNames) > 0 {
		networksNode, err := encodeStringSliceNode(networkNames)
		if err != nil {
			return nil, err
		}
		appendMapping(root, "networks", networksNode)
	}

	return root, nil
}

func (m *Project) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected manifest mapping, got YAML kind %d", node.Kind)
	}

	var out Project
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]

		switch key {
		case "x-envy":
			if err := value.Decode(&out.Meta); err != nil {
				return err
			}
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
		default:
			if strings.HasPrefix(key, "x-set-") {
				setKey := strings.TrimPrefix(key, "x-set-")
				set, err := decodeSetNode(value)
				if err != nil {
					return err
				}

				description, link := extractSetMetadataFromComments(node, i)
				if set.Description == "" {
					set.Description = description
				}
				if set.Link == "" {
					set.Link = link
				}

				if out.Sets == nil {
					out.Sets = make(map[string]Set)
				}
				out.Sets[setKey] = set
				continue
			}

			switch key {
			case "volumes":
				volumes, err := decodeNamedEntriesNode(value)
				if err != nil {
					return err
				}
				out.setVolumeNames(volumes)
			case "networks":
				var networks []string
				if err := value.Decode(&networks); err != nil {
					return err
				}
				out.setNetworkNames(networks)
			}
			continue
		}
	}

	*m = out
	if m.Sets == nil {
		m.Sets = make(map[string]Set)
	}
	m.ensureComposeProject()
	return nil
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
			sets, err := decodeStringSliceOrScalar(value)
			if err != nil {
				return err
			}
			out.Sets = sets
		case "environment":
			sets, err := decodeServiceEnvironmentSetRefs(value)
			if err != nil {
				return err
			}
			if len(sets) > 0 {
				out.Sets = sets
			}
		}
	}

	*s = out
	return nil
}

func decodeStringSliceOrScalar(node *yaml.Node) ([]string, error) {
	if node == nil {
		return nil, nil
	}

	switch node.Kind {
	case yaml.SequenceNode:
		var items []string
		if err := node.Decode(&items); err != nil {
			return nil, err
		}
		return items, nil
	case yaml.ScalarNode:
		var item string
		if err := node.Decode(&item); err != nil {
			return nil, err
		}
		if strings.TrimSpace(item) == "" {
			return nil, nil
		}
		return []string{item}, nil
	default:
		return nil, fmt.Errorf("expected string or list of strings, got YAML kind %d", node.Kind)
	}
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

// OrderedSets returns all sets in deterministic order.
func (m *Project) OrderedSets() []Set {
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

func (v Var) IsReadonly() bool {
	return strings.EqualFold(strings.TrimSpace(v.Readonly), "true")
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
	if v.IsReadonly() {
		appendMapping(node, "readonly", &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: "true"})
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
			appendMapping(setNode, v.Key, encoded)
		}
	}
	return setNode, nil
}

func encodeServiceNode(svc Service, setNodes map[string]*yaml.Node) (*yaml.Node, error) {
	serviceNode := &yaml.Node{Kind: yaml.MappingNode}

	if svc.Image != "" {
		appendMapping(serviceNode, "image", &yaml.Node{Kind: yaml.ScalarNode, Value: svc.Image})
	}
	if svc.Platform != "" {
		appendMapping(serviceNode, "platform", &yaml.Node{Kind: yaml.ScalarNode, Value: svc.Platform})
	}
	if len(svc.Entrypoint) > 0 {
		entrypointNode := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, item := range svc.Entrypoint {
			entrypointNode.Content = append(entrypointNode.Content, &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: item})
		}
		appendMapping(serviceNode, "entrypoint", entrypointNode)
	}
	if len(svc.Command) > 0 {
		commandNode := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, item := range svc.Command {
			commandNode.Content = append(commandNode.Content, &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: item})
		}
		appendMapping(serviceNode, "command", commandNode)
	}
	if svc.Description != "" {
		appendMapping(serviceNode, "description", &yaml.Node{Kind: yaml.ScalarNode, Value: svc.Description})
	}

	if len(svc.Sets) > 0 {
		environmentNode := &yaml.Node{Kind: yaml.MappingNode}
		mergeNode := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, setKey := range svc.Sets {
			setNode, ok := setNodes[setKey]
			if !ok {
				continue
			}
			mergeNode.Content = append(mergeNode.Content, &yaml.Node{Kind: yaml.AliasNode, Value: setNode.Anchor, Alias: setNode})
		}
		if len(mergeNode.Content) > 0 {
			appendMapping(environmentNode, "<<", mergeNode)
			appendMapping(serviceNode, "environment", environmentNode)
		}
	}

	return serviceNode, nil
}

func decodeServicesNode(node *yaml.Node) (Services, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected services mapping, got YAML kind %d", node.Kind)
	}

	services := make(Services, 0, len(node.Content)/2)
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
		default:
			if value.Kind == yaml.ScalarNode {
				var scalar string
				if err := value.Decode(&scalar); err != nil {
					return out, err
				}
				v := normalizeVarComposeSyntax(Var{Key: key, Default: scalar})
				out.Vars = append(out.Vars, v)
				continue
			}

			var v Var
			if err := value.Decode(&v); err != nil {
				return out, err
			}
			v.Key = key
			out.Vars = append(out.Vars, normalizeVarComposeSyntax(v))
		}
	}
	return out, nil
}

func parseSetMetadataFromComments(comments ...string) (string, string) {
	var descriptionParts []string
	link := ""

	for _, raw := range comments {
		for _, line := range strings.Split(raw, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}

			clean := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			if clean == "" {
				continue
			}

			if link == "" {
				if matches := markdownLinkPattern.FindStringSubmatch(clean); len(matches) == 2 {
					link = matches[1]
					continue
				}
				if markdownReferenceLinkPattern.MatchString(clean) {
					link = clean
					continue
				}
				if matches := prefixedLinkPattern.FindStringSubmatch(clean); len(matches) == 2 {
					link = matches[1]
					continue
				}
				if plainLinkPattern.MatchString(clean) {
					link = clean
					continue
				}
			}

			descriptionParts = append(descriptionParts, clean)
		}
	}

	return strings.TrimSpace(strings.Join(descriptionParts, " ")), link
}

func extractSetMetadataFromComments(root *yaml.Node, keyIndex int) (string, string) {
	if root == nil || keyIndex < 0 || keyIndex+1 >= len(root.Content) {
		return "", ""
	}

	keyNode := root.Content[keyIndex]
	valueNode := root.Content[keyIndex+1]

	comments := []string{
		keyNode.HeadComment,
		keyNode.LineComment,
		valueNode.HeadComment,
		valueNode.LineComment,
	}

	// Comments between mapping entries can be attached to the previous value node.
	if keyIndex >= 2 {
		prevValueNode := root.Content[keyIndex-1]
		comments = append([]string{prevValueNode.FootComment}, comments...)
	}

	return parseSetMetadataFromComments(comments...)
}

func decodeServiceEnvironmentSetRefs(node *yaml.Node) ([]string, error) {
	if node == nil {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, nil
	}

	seen := make(map[string]struct{})
	var sets []string

	addSet := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		sets = append(sets, name)
	}

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if key != "<<" {
			continue
		}
		value := node.Content[i+1]

		collectFromNode := func(n *yaml.Node) {
			if n == nil {
				return
			}
			switch n.Kind {
			case yaml.AliasNode:
				if n.Alias != nil {
					addSet(n.Alias.Anchor)
				}
				if strings.HasPrefix(n.Value, "*") {
					addSet(strings.TrimPrefix(n.Value, "*"))
				}
			case yaml.MappingNode:
				addSet(n.Anchor)
			case yaml.ScalarNode:
				if strings.HasPrefix(n.Value, "*") {
					addSet(strings.TrimPrefix(n.Value, "*"))
				}
			}
		}

		switch value.Kind {
		case yaml.SequenceNode:
			for _, item := range value.Content {
				collectFromNode(item)
			}
		default:
			collectFromNode(value)
		}
	}

	return sets, nil
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
			vars = append(vars, normalizeVarComposeSyntax(v))
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
			vars = append(vars, normalizeVarComposeSyntax(v))
		}
		return vars, nil
	default:
		return nil, fmt.Errorf("expected vars sequence or mapping, got YAML kind %d", node.Kind)
	}
}

func normalizeVarComposeSyntax(v Var) Var {
	parsedDefault, parsedRequired, parsedReadonly := parseComposeDefaultSyntax(v.Default)
	v.Default = parsedDefault

	if strings.TrimSpace(v.Required) == "" {
		if parsedRequired {
			v.Required = "true"
		} else {
			v.Required = "false"
		}
	}

	if strings.TrimSpace(v.Readonly) == "" {
		if parsedReadonly {
			v.Readonly = "true"
		} else {
			v.Readonly = "false"
		}
	}

	return v
}

func parseComposeDefaultSyntax(value string) (defaultValue string, required bool, readonly bool) {
	value = strings.TrimSpace(value)
	matches := composeInterpolationPattern.FindStringSubmatch(value)
	if len(matches) == 0 {
		return value, false, true
	}

	operator := matches[2]
	rawDefault := matches[3]

	switch operator {
	case "":
		return "", true, false
	case ":-", "-":
		return rawDefault, false, false
	case ":?", "?":
		return rawDefault, true, false
	default:
		return value, false, true
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
func (m *Project) AllVars() []Var {
	var vars []Var
	for _, set := range m.OrderedSets() {
		vars = append(vars, set.Vars...)
	}
	return vars
}

func (i LintIssue) String() string {
	if i.Path == "" {
		return fmt.Sprintf("[%s] %s: %s", strings.ToUpper(i.Level), i.Rule, i.Message)
	}
	return fmt.Sprintf("[%s] %s: %s: %s", strings.ToUpper(i.Level), i.Rule, i.Path, i.Message)
}

// VarsForServiceBySet returns vars per set for a service, preserving set order.
func (m *Project) VarsForServiceBySet(serviceName string) []SetVars {
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
func (m *Project) VarsForService(serviceName string) []Var {
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
func (m *Project) SecretVars() []Var {
	var vars []Var
	for _, v := range m.AllVars() {
		if v.IsSecret() {
			vars = append(vars, v)
		}
	}
	return vars
}

// RequiredVars returns only variables marked required.
func (m *Project) RequiredVars() []Var {
	var vars []Var
	for _, v := range m.AllVars() {
		if v.IsRequired() {
			vars = append(vars, v)
		}
	}
	return vars
}

// Lint returns warnings for values that are legal but potentially ambiguous.
func (m *Project) Lint() []string {
	issues := m.LintIssues()
	warnings := make([]string, 0, len(issues))
	for _, issue := range issues {
		warnings = append(warnings, issue.String())
	}
	return warnings
}

// LintIssues returns DCLint-inspired lint findings with severity and rule IDs.
func (m *Project) LintIssues() []LintIssue {
	var issues []LintIssue
	seenServices := make(map[string]bool)
	usedSets := make(map[string]bool)

	if strings.TrimSpace(m.Meta.Title) == "" {
		issues = append(issues, LintIssue{
			Level:   "warning",
			Rule:    "require-project-name-field",
			Path:    "x-envy.title",
			Message: "project name is not set",
		})
	}

	if !areServiceNamesSorted(m.Services) {
		issues = append(issues, LintIssue{
			Level:   "warning",
			Rule:    "services-alphabetical-order",
			Path:    "services",
			Message: "services should be sorted alphabetically",
		})
	}

	for _, svc := range m.Services {
		if strings.TrimSpace(svc.Name) == "" {
			issues = append(issues, LintIssue{
				Level:   "error",
				Rule:    "service-name-not-empty",
				Path:    "services",
				Message: "found service with empty name",
			})
			continue
		}

		if seenServices[svc.Name] {
			issues = append(issues, LintIssue{
				Level:   "error",
				Rule:    "no-duplicate-service-names",
				Path:    fmt.Sprintf("services.%s", svc.Name),
				Message: "duplicate service name",
			})
		}
		seenServices[svc.Name] = true

		if strings.TrimSpace(svc.Image) == "" {
			issues = append(issues, LintIssue{
				Level:   "error",
				Rule:    "service-image-require-explicit-tag",
				Path:    fmt.Sprintf("services.%s.image", svc.Name),
				Message: "no image configured",
			})
		} else if !isValidImageReference(strings.TrimSpace(svc.Image)) {
			issues = append(issues, LintIssue{
				Level:   "error",
				Rule:    "service-image-require-explicit-tag",
				Path:    fmt.Sprintf("services.%s.image", svc.Name),
				Message: fmt.Sprintf("invalid image %q - expected docker image reference like ghcr.io/org/app:tag", svc.Image),
			})
		} else if !hasExplicitImageTag(strings.TrimSpace(svc.Image)) {
			issues = append(issues, LintIssue{
				Level:   "error",
				Rule:    "service-image-require-explicit-tag",
				Path:    fmt.Sprintf("services.%s.image", svc.Name),
				Message: fmt.Sprintf("image %q has no explicit tag or digest", svc.Image),
			})
		} else if hasUnstableImageTag(strings.TrimSpace(svc.Image)) {
			issues = append(issues, LintIssue{
				Level:   "warning",
				Rule:    "service-image-require-explicit-tag",
				Path:    fmt.Sprintf("services.%s.image", svc.Name),
				Message: fmt.Sprintf("image %q uses unstable tag; avoid latest/stable/dev/main/master", svc.Image),
			})
		}

		platform := strings.TrimSpace(svc.Platform)
		if platform != "" && !isValidPlatform(platform) {
			issues = append(issues, LintIssue{
				Level:   "warning",
				Rule:    "service-platform-format",
				Path:    fmt.Sprintf("services.%s.platform", svc.Name),
				Message: fmt.Sprintf("invalid platform %q - expected os/arch or os/arch/variant", platform),
			})
		}

		for _, setKey := range svc.Sets {
			usedSets[setKey] = true
			if _, ok := m.Sets[setKey]; !ok {
				issues = append(issues, LintIssue{
					Level:   "warning",
					Rule:    "service-references-existing-set",
					Path:    fmt.Sprintf("services.%s.environment", svc.Name),
					Message: fmt.Sprintf("unknown set %q", setKey),
				})
			}
		}
	}

	for setKey := range m.Sets {
		if usedSets[setKey] {
			continue
		}
		issues = append(issues, LintIssue{
			Level:   "error",
			Rule:    "x-set-anchor-must-be-used",
			Path:    fmt.Sprintf("x-set-%s", setKey),
			Message: fmt.Sprintf("set %q is not referenced by any service", setKey),
		})
	}

	return issues
}

func areServiceNamesSorted(services Services) bool {
	if len(services) < 2 {
		return true
	}
	for i := 1; i < len(services); i++ {
		if services[i-1].Name > services[i].Name {
			return false
		}
	}
	return true
}

func hasExplicitImageTag(image string) bool {
	if strings.Contains(image, "@") {
		return true
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	return lastColon > lastSlash
}

func hasUnstableImageTag(image string) bool {
	if strings.Contains(image, "@") {
		return false
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash {
		return false
	}
	tag := strings.ToLower(strings.TrimSpace(image[lastColon+1:]))
	switch tag {
	case "latest", "stable", "dev", "main", "master":
		return true
	default:
		return false
	}
}

// Load reads and parses an compose.yaml file.
func Load(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", path, err)
	}
	var m Project
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
	}
	return &m, nil
}
