package compose

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// mergeKey is the YAML merge key ("<<") used to combine x-set anchors into a
// service's environment.
const mergeKey = "<<"

// ErrRenderInput is returned when the compose file cannot be rendered, e.g.
// because it is not a YAML mapping or a merge key does not refer to mappings.
var ErrRenderInput = errors.New("invalid compose file for rendering")

// Render returns a plain Compose file for tools that do not handle YAML
// anchors well (e.g. deployment platforms that re-serialize the file):
//
//   - aliases are replaced by copies of their anchored nodes,
//   - merge keys ("<<: *set" and "<<: [*a, *b]") are expanded in place,
//   - top-level extension fields ("x-*", including x-envy and x-set-*) are
//     removed,
//   - comments and anchor names are dropped.
//
// Values are not interpolated: "${VAR}" and "${VAR:-default}" are kept as
// they are, so the result is still configured through the environment.
// Key order is preserved. header, if not empty, is written as a comment at
// the top of the result.
func Render(data []byte, header string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRenderInput, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: top level must be a mapping", ErrRenderInput)
	}

	root, err := resolveNode(doc.Content[0])
	if err != nil {
		return nil, err
	}
	root.Content = withoutExtensionFields(root.Content)

	var buf bytes.Buffer
	for _, line := range strings.Split(strings.TrimSpace(header), "\n") {
		if line == "" {
			continue
		}
		buf.WriteString("# " + line + "\n")
	}

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("encoding rendered compose file: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encoding rendered compose file: %w", err)
	}
	return buf.Bytes(), nil
}

// resolveNode returns a deep copy of node with aliases and merge keys resolved
// and comments and anchors removed.
func resolveNode(node *yaml.Node) (*yaml.Node, error) {
	switch node.Kind {
	case yaml.AliasNode:
		if node.Alias == nil {
			return nil, fmt.Errorf("%w: unknown alias *%s (line %d)", ErrRenderInput, node.Value, node.Line)
		}
		return resolveNode(node.Alias)
	case yaml.MappingNode:
		return resolveMapping(node)
	case yaml.SequenceNode, yaml.DocumentNode:
		out := copyNode(node)
		out.Content = make([]*yaml.Node, 0, len(node.Content))
		for _, child := range node.Content {
			resolved, err := resolveNode(child)
			if err != nil {
				return nil, err
			}
			out.Content = append(out.Content, resolved)
		}
		return out, nil
	default:
		return copyNode(node), nil
	}
}

// resolveMapping expands merge keys in place. Following the YAML merge key
// specification, keys defined in the mapping itself take precedence over
// merged keys, and earlier mappings in a merge list take precedence over later
// ones.
func resolveMapping(node *yaml.Node) (*yaml.Node, error) {
	explicit := make(map[string]bool, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		if key := node.Content[i]; !isMergeKey(key) {
			explicit[key.Value] = true
		}
	}

	out := copyNode(node)
	out.Content = make([]*yaml.Node, 0, len(node.Content))
	merged := make(map[string]bool)

	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]

		if !isMergeKey(key) {
			resolvedValue, err := resolveNode(value)
			if err != nil {
				return nil, err
			}
			out.Content = append(out.Content, copyNode(key), resolvedValue)
			continue
		}

		sources, err := mergeSources(value)
		if err != nil {
			return nil, err
		}
		for _, source := range sources {
			resolved, err := resolveNode(source)
			if err != nil {
				return nil, err
			}
			if resolved.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("%w: merge key at line %d must refer to a mapping", ErrRenderInput, key.Line)
			}
			for j := 0; j+1 < len(resolved.Content); j += 2 {
				name := resolved.Content[j].Value
				if explicit[name] || merged[name] {
					continue
				}
				merged[name] = true
				out.Content = append(out.Content, resolved.Content[j], resolved.Content[j+1])
			}
		}
	}
	return out, nil
}

// mergeSources returns the mappings referenced by a merge key value: a single
// alias or mapping, or a sequence of them.
func mergeSources(value *yaml.Node) ([]*yaml.Node, error) {
	switch value.Kind {
	case yaml.AliasNode, yaml.MappingNode:
		return []*yaml.Node{value}, nil
	case yaml.SequenceNode:
		return value.Content, nil
	default:
		return nil, fmt.Errorf("%w: merge key at line %d must refer to a mapping or a list of mappings", ErrRenderInput, value.Line)
	}
}

// isMergeKey reports whether key is an unquoted "<<" (yaml.v3 tags it
// !!merge); a quoted "<<" is an ordinary string key.
func isMergeKey(key *yaml.Node) bool {
	if key.Kind != yaml.ScalarNode {
		return false
	}
	return key.Tag == "!!merge" || (key.Value == mergeKey && key.Style == 0 && key.Tag != "!!str")
}

// withoutExtensionFields drops top-level "x-*" key/value pairs.
func withoutExtensionFields(content []*yaml.Node) []*yaml.Node {
	out := make([]*yaml.Node, 0, len(content))
	for i := 0; i+1 < len(content); i += 2 {
		if strings.HasPrefix(content[i].Value, "x-") {
			continue
		}
		out = append(out, content[i], content[i+1])
	}
	return out
}

// copyNode returns a shallow copy of node without anchor and comments.
func copyNode(node *yaml.Node) *yaml.Node {
	out := *node
	out.Anchor = ""
	out.HeadComment = ""
	out.LineComment = ""
	out.FootComment = ""
	return &out
}
