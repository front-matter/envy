// Package writer renders env.yaml into a .env file.
package writer

import (
	"fmt"
	"strings"

	"github.com/front-matter/envy/manifest"
)

// Options controls what the writer emits.
type Options struct {
	IncludeSecrets bool
}

// Generate renders env.yaml into a documented .env string.
func Generate(m *manifest.Manifest, opts Options) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s — Environment Configuration\n", m.Meta.Name))
	sb.WriteString("# Generated from env.yaml — edit env.yaml, not this file.\n")
	sb.WriteString(fmt.Sprintf("# Version: %s\n", m.Meta.VersionLabel()))
	sb.WriteString(fmt.Sprintf("# Docs: %s\n", m.Meta.Docs))
	sb.WriteString("\n")

	for _, group := range m.OrderedGroups() {
		dashes := strings.Repeat("─", dashWidth(group.Key))
		sb.WriteString(fmt.Sprintf("# ── %s %s\n", group.Key, dashes))
		sb.WriteString(fmt.Sprintf("# %s\n", group.Description))

		for _, v := range group.Vars {
			if v.Secret && !opts.IncludeSecrets {
				sb.WriteString(fmt.Sprintf(
					"# %s=  # SECRET — inject via SOPS or environment\n", v.Key,
				))
				continue
			}

			desc := strings.ReplaceAll(strings.TrimSpace(v.Description), "\n", " ")
			if v.Required {
				sb.WriteString(fmt.Sprintf("# [REQUIRED] %s\n", desc))
			} else {
				sb.WriteString(fmt.Sprintf("# %s\n", desc))
			}

			if v.Example != "" {
				sb.WriteString(fmt.Sprintf("# Example: %s\n", v.Example))
			}

			sb.WriteString(fmt.Sprintf("%s=%s\n", v.Key, v.Default.String()))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func dashWidth(name string) int {
	w := 51 - len(name)
	if w < 0 {
		return 0
	}
	return w
}
