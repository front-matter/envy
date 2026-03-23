// Package compose loads and provides access to the compose.yaml spec.
package compose

import (
	"fmt"
	"go/doc/comment"
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

// Project is the top-level structure used by envy and contains everything.
type Project struct {
	*types.Project
	Node     *yaml.Node         `yaml:"-"`
	Meta     Meta               `yaml:"x-envy"`
	Services map[string]Service `yaml:"services"`
	Sets     map[string]Set     `yaml:"-"`
}

// Service describes a compose service and the sets it uses.
type Service struct {
	Name        string   `yaml:"name"`
	Image       string   `yaml:"image,omitempty"`
	Platform    string   `yaml:"platform,omitempty"`
	Entrypoint  []string `yaml:"entrypoint,omitempty"`
	Command     []string `yaml:"command,omitempty"`
	Profiles    []string `yaml:"profiles,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Sets        []string `yaml:"-"`
}

// AnnotatedEnv is a helper type to decode environment variables with comments.
type AnnotatedEnv struct {
	Mapping  types.MappingWithEquals
	Comments map[string]string // key → comment text
}

// UnmarshalYAML decodes environment variables from a YAML sequence while capturing comments.
func (a *AnnotatedEnv) UnmarshalYAML(value *yaml.Node) error {
	a.Comments = make(map[string]string)

	// value is a sequence node (environment is a list)
	if value.Kind != yaml.SequenceNode {
		return fmt.Errorf("expected sequence, got %v", value.Kind)
	}

	var raw []string
	for _, item := range value.Content {
		// item.Value is the string e.g. "PORT=8080"
		// item.LineComment is "# my comment" (with the # included)
		raw = append(raw, item.Value)

		if item.LineComment != "" {
			key := extractKey(item.Value)
			a.Comments[key] = strings.TrimPrefix(
				strings.TrimSpace(item.LineComment), "# ",
			)
		}
		if item.HeadComment != "" {
			key := extractKey(item.Value)
			a.Comments[key+"_head"] = strings.TrimPrefix(
				strings.TrimSpace(item.HeadComment), "# ",
			)
		}
	}

	a.Mapping = types.NewMappingWithEquals(raw)
	return nil
}

// extractKey extracts the variable name from a "KEY=VALUE" string, returning "KEY".
func extractKey(pair string) string {
	k, _, _ := strings.Cut(pair, "=")
	return k
}

// cleanComment trims whitespace and leading # from a comment string.
func cleanComment(c string) string {
	c = strings.TrimSpace(c)
	c = strings.TrimPrefix(c, "#")
	return strings.TrimSpace(c)
}

// ServiceConfigWithComments extends compose-go's ServiceConfig
// to include environment variables with comments.
type ServiceConfigWithComments struct {
	types.ServiceConfig `yaml:",inline"`
	Environment         AnnotatedEnv `yaml:"environment"`
}

type ComposeFileWithComments struct {
	Services map[string]ServiceConfigWithComments `yaml:"services"`
}

// ParseWithComments parses a compose YAML file while preserving comments on environment variables.
func ParseWithComments(data []byte) (*ComposeFileWithComments, error) {
	var out ComposeFileWithComments
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
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

type serviceYAML struct {
	Image       string     `yaml:"image,omitempty"`
	Platform    string     `yaml:"platform,omitempty"`
	Entrypoint  []string   `yaml:"entrypoint,omitempty"`
	Command     *yaml.Node `yaml:"command,omitempty"`
	Profiles    *yaml.Node `yaml:"profiles,omitempty"`
	Description string     `yaml:"description,omitempty"`
	Sets        *yaml.Node `yaml:"sets,omitempty"`
}

// Set is a logical grouping of related env variables
// aligning with compose-go's MappingWithEquals to
// leverage YAML encoding/decoding and preserve field order.
type Set types.MappingWithEquals

const setInternalPrefix = "__envy_set."

func setMetadataKey(name string) string {
	return setInternalPrefix + name
}

func setVarsStorageKey() string {
	return setInternalPrefix + "vars"
}

// Var aligns with compose-go's MappingWithEquals value type.
type Var *string

// SetVars holds vars for a single set.
type SetVars struct {
	SetKey      string
	Description string
	Vars        types.MappingWithEquals
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
		if len(set.Vars()) > 0 {
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
			filtered, ok := filteredSets[set.Key()]
			if !ok {
				continue
			}
			setNode, err := encodeSetNode(filtered)
			if err != nil {
				return nil, err
			}
			setNode.Anchor = set.Key()
			setNodes[set.Key()] = setNode
			appendMapping(root, "x-set-"+set.Key(), setNode)
		}
	}

	if len(m.Services) > 0 {
		servicesNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, serviceName := range m.OrderedServiceNames() {
			svc := m.Services[serviceName]
			if strings.TrimSpace(svc.Name) == "" {
				svc.Name = serviceName
			}
			encoded, err := encodeServiceNode(svc, setNodes)
			if err != nil {
				return nil, err
			}
			appendMapping(servicesNode, serviceName, encoded)
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
				if set.Description() == "" {
					set.SetDescription(description)
				}
				if set.Link() == "" {
					set.SetLink(link)
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
	if m.Services == nil {
		m.Services = map[string]Service{}
	}
	if m.Sets == nil {
		m.Sets = make(map[string]Set)
	}
	m.ensureComposeProject()
	return nil
}

// OrderedServiceNames returns sorted service names from map-based services.
func (m *Project) OrderedServiceNames() []string {
	if m == nil || len(m.Services) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.Services))
	for key := range m.Services {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// OrderedServices returns services in deterministic order and normalizes empty Name from key.
func (m *Project) OrderedServices() []Service {
	keys := m.OrderedServiceNames()
	if len(keys) == 0 {
		return nil
	}
	services := make([]Service, 0, len(keys))
	for _, key := range keys {
		svc := m.Services[key]
		if strings.TrimSpace(svc.Name) == "" {
			svc.Name = key
		}
		services = append(services, svc)
	}
	return services
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

	if len(s.Profiles) > 0 {
		node := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, item := range s.Profiles {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: item})
		}
		out.Profiles = node
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
		case "profiles":
			profiles, err := decodeStringSliceOrScalar(value)
			if err != nil {
				return err
			}
			out.Profiles = profiles
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
		set.SetKey(key)
		sets = append(sets, set)
	}

	return sets
}

func NewSet() Set {
	return Set{}
}

func (s *Set) ensureMap() types.MappingWithEquals {
	if *s == nil {
		*s = Set{}
	}
	return types.MappingWithEquals(*s)
}

func (s Set) get(key string) string {
	m := types.MappingWithEquals(s)
	if m == nil {
		return ""
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	return *raw
}

func (s *Set) set(key, value string) {
	m := s.ensureMap()
	v := value
	m[key] = &v
}

func (s *Set) delete(key string) {
	delete(s.ensureMap(), key)
}

func (s Set) Key() string         { return s.get(setMetadataKey("key")) }
func (s Set) Description() string { return s.get(setMetadataKey("description")) }
func (s Set) Link() string        { return s.get(setMetadataKey("link")) }
func (s Set) Vars() types.MappingWithEquals {
	m := types.MappingWithEquals(s)
	if m == nil {
		return nil
	}

	vars := types.MappingWithEquals{}
	for key, value := range m {
		if strings.HasPrefix(key, setInternalPrefix) {
			continue
		}
		if strings.TrimSpace(key) == "" {
			continue
		}
		if value == nil {
			vars[key] = nil
			continue
		}
		v := *value
		vars[key] = &v
	}

	if len(vars) == 0 {
		return nil
	}

	return vars
}

func (s *Set) SetKey(value string)         { s.set(setMetadataKey("key"), value) }
func (s *Set) SetDescription(value string) { s.set(setMetadataKey("description"), value) }
func (s *Set) SetLink(value string)        { s.set(setMetadataKey("link"), value) }
func (s *Set) SetVars(vars types.MappingWithEquals) {
	m := s.ensureMap()
	for key := range m {
		if strings.HasPrefix(key, setInternalPrefix) {
			continue
		}
		delete(m, key)
	}

	for key, value := range vars {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		if value == nil {
			m[trimmedKey] = nil
			continue
		}
		normalized := parseComposeDefaultSyntax(*value)
		v := normalized
		m[trimmedKey] = &v
	}
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
	if set.Description() != "" {
		appendMapping(setNode, "description", &yaml.Node{Kind: yaml.ScalarNode, Value: set.Description()})
	}
	if set.Link() != "" {
		appendMapping(setNode, "link", &yaml.Node{Kind: yaml.ScalarNode, Value: set.Link()})
	}
	if len(set.Vars()) > 0 {
		for _, key := range sortedMappingKeys(set.Vars()) {
			value := ""
			if raw := set.Vars()[key]; raw != nil {
				value = *raw
			}
			appendMapping(setNode, key, &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: value})
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

func decodeServicesNode(node *yaml.Node) (map[string]Service, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected services mapping, got YAML kind %d", node.Kind)
	}

	services := make(map[string]Service, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		name := node.Content[i].Value
		var svc Service
		if err := node.Content[i+1].Decode(&svc); err != nil {
			return nil, err
		}
		svc.Name = name
		if strings.TrimSpace(svc.Description) == "" {
			svc.Description = extractServiceDescriptionFromComments(node, i)
		}
		services[name] = svc
	}
	return services, nil
}

func parseServiceDescriptionFromComments(comments ...string) string {
	var blocks []string
	var currentParagraph []string

	flushParagraph := func() {
		if len(currentParagraph) == 0 {
			return
		}
		blocks = append(blocks, strings.Join(currentParagraph, " "))
		currentParagraph = nil
	}

	for _, raw := range comments {
		for _, line := range strings.Split(raw, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}

			clean := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			if clean == "" {
				flushParagraph()
				continue
			}

			if isSetCommentLinkOnlyLine(clean) {
				flushParagraph()
				if extracted := extractFirstCommentLink(clean); extracted != "" {
					blocks = append(blocks, extracted)
				} else {
					blocks = append(blocks, clean)
				}
				continue
			}

			currentParagraph = append(currentParagraph, clean)
		}
	}

	flushParagraph()

	return strings.TrimSpace(strings.Join(blocks, "\n"))
}

func extractServiceDescriptionFromComments(root *yaml.Node, keyIndex int) string {
	if root == nil || keyIndex < 0 || keyIndex+1 >= len(root.Content) {
		return ""
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

	return parseServiceDescriptionFromComments(comments...)
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
	out := NewSet()
	if node.Kind != yaml.MappingNode {
		return out, fmt.Errorf("expected set mapping, got YAML kind %d", node.Kind)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]
		switch key {
		case "description":
			var description string
			if err := value.Decode(&description); err != nil {
				return out, err
			}
			out.SetDescription(description)
		case "link":
			var link string
			if err := value.Decode(&link); err != nil {
				return out, err
			}
			out.SetLink(link)
		case "vars":
			vars, err := decodeVarsNode(value)
			if err != nil {
				return out, err
			}
			out.SetVars(vars)
		default:
			vars := out.Vars()
			if vars == nil {
				vars = types.MappingWithEquals{}
			}
			defaultValue, err := decodeVarDefault(value)
			if err != nil {
				return out, err
			}
			v := defaultValue
			vars[key] = &v
			out.SetVars(vars)
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
				if extracted := extractFirstCommentLink(clean); extracted != "" {
					link = extracted
				}
			}

			if isSetCommentLinkOnlyLine(clean) {
				continue
			}

			descriptionParts = append(descriptionParts, clean)
		}
	}

	return strings.TrimSpace(strings.Join(descriptionParts, " ")), link
}

func extractFirstCommentLink(text string) string {
	var parser comment.Parser
	doc := parser.Parse(text)

	for _, block := range doc.Content {
		paragraph, ok := block.(*comment.Paragraph)
		if !ok {
			continue
		}
		for _, item := range paragraph.Text {
			link, ok := item.(*comment.Link)
			if ok && strings.TrimSpace(link.URL) != "" {
				return strings.TrimSpace(link.URL)
			}
		}
	}

	for _, def := range doc.Links {
		if def != nil && strings.TrimSpace(def.URL) != "" {
			return strings.TrimSpace(def.URL)
		}
	}

	return ""
}

func isSetCommentLinkOnlyLine(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	if plainLinkPattern.MatchString(trimmed) {
		return true
	}
	if markdownReferenceLinkPattern.MatchString(trimmed) {
		return true
	}
	if matches := prefixedLinkPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		return true
	}
	if matches := markdownLinkPattern.FindStringSubmatch(trimmed); len(matches) == 2 && matches[0] == trimmed {
		return true
	}

	return false
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

func decodeVarsNode(node *yaml.Node) (types.MappingWithEquals, error) {
	switch node.Kind {
	case yaml.SequenceNode:
		vars := types.MappingWithEquals{}
		for _, item := range node.Content {
			var decoded map[string]string
			if err := item.Decode(&decoded); err != nil {
				return nil, err
			}
			key := strings.TrimSpace(decoded["key"])
			if key == "" {
				continue
			}
			v := parseComposeDefaultSyntax(decoded["default"])
			vars[key] = &v
		}
		return vars, nil
	case yaml.MappingNode:
		vars := types.MappingWithEquals{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			defaultValue, err := decodeVarDefault(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			v := defaultValue
			vars[key] = &v
		}
		return vars, nil
	default:
		return nil, fmt.Errorf("expected vars sequence or mapping, got YAML kind %d", node.Kind)
	}
}

func decodeVarDefault(node *yaml.Node) (string, error) {
	if node.Kind == yaml.ScalarNode {
		var scalar string
		if err := node.Decode(&scalar); err != nil {
			return "", err
		}
		return parseComposeDefaultSyntax(scalar), nil
	}

	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value != "default" {
				continue
			}
			var scalar string
			if err := node.Content[i+1].Decode(&scalar); err != nil {
				return "", err
			}
			return parseComposeDefaultSyntax(scalar), nil
		}
		return "", nil
	}

	return "", fmt.Errorf("expected var default scalar or mapping, got YAML kind %d", node.Kind)
}

func sortedMappingKeys(values types.MappingWithEquals) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// SortedVarKeys returns stable sorted keys for map-based vars.
func SortedVarKeys(vars types.MappingWithEquals) []string {
	return sortedMappingKeys(vars)
}

// VarString returns the dereferenced value or an empty string.
func VarString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func parseComposeDefaultSyntax(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	matches := composeInterpolationPattern.FindStringSubmatch(value)
	if len(matches) == 0 {
		return value
	}

	operator := matches[2]
	rawDefault := matches[3]

	switch operator {
	case "":
		return ""
	case ":-", "-":
		return rawDefault
	case ":?", "?":
		return rawDefault
	default:
		return value
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

// AllVars returns merged vars across all sets.
func (m *Project) AllVars() types.MappingWithEquals {
	vars := types.MappingWithEquals{}
	for _, set := range m.OrderedSets() {
		for key, value := range set.Vars() {
			if value == nil {
				vars[key] = nil
				continue
			}
			v := *value
			vars[key] = &v
		}
	}
	if len(vars) == 0 {
		return nil
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
			result = append(result, SetVars{SetKey: set.Key(), Description: set.Description(), Vars: set.Vars()})
		}
		return result
	}

	for _, svc := range m.OrderedServices() {
		if svc.Name != name {
			continue
		}
		var result []SetVars
		for _, setKey := range svc.Sets {
			set, ok := m.Sets[setKey]
			if !ok {
				continue
			}
			result = append(result, SetVars{SetKey: setKey, Description: set.Description(), Vars: set.Vars()})
		}
		return result
	}

	var result []SetVars
	for _, set := range m.OrderedSets() {
		result = append(result, SetVars{SetKey: set.Key(), Description: set.Description(), Vars: set.Vars()})
	}
	return result
}

// VarsForService returns vars for one service, or all vars if service is unknown.
func (m *Project) VarsForService(serviceName string) types.MappingWithEquals {
	name := strings.TrimSpace(serviceName)
	if name == "" {
		return m.AllVars()
	}

	for _, svc := range m.OrderedServices() {
		if svc.Name != name {
			continue
		}

		vars := types.MappingWithEquals{}
		for _, setKey := range svc.Sets {
			set, ok := m.Sets[setKey]
			if !ok {
				continue
			}
			for key, value := range set.Vars() {
				if value == nil {
					vars[key] = nil
					continue
				}
				v := *value
				vars[key] = &v
			}
		}
		if len(vars) == 0 {
			return nil
		}
		return vars
	}

	return m.AllVars()
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

	for _, svc := range m.OrderedServices() {
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

func areServiceNamesSorted(services map[string]Service) bool {
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

// Load reads a compose file from the given path, parses it into a Project,
// and preserves the raw YAML node for comment/round-trip access.
func Load(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading compose file %s: %w", path, err)
	}

	// Parse into a raw yaml.Node to preserve everything (comments, ordering, etc.)
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing compose file %s: %w", path, err)
	}

	// Now unmarshal into Project structure
	var p Project
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decoding compose file %s: %w", path, err)
	}

	// Store the raw node for potential round-trip use
	p.Node = &root

	return &p, nil
}
