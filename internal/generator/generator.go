// Package generator renders env.yaml into a .env file.
package generator

import (
	"fmt"
	"strings"

	"github.com/front-matter/envy/internal/manifest"
)

// Options controls what the generator emits.
type Options struct {
	IncludeSecrets bool
}

// Generate renders env.yaml into a documented .env string.
func Generate(m *manifest.Manifest, opts Options) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s — InvenioRDM Environment Configuration\n", m.Meta.Name))
	sb.WriteString("# Generated from env.yaml — edit env.yaml, not this file.\n")
	sb.WriteString(fmt.Sprintf("# Invenio version: %s\n", m.Meta.InvenioVersion))
	sb.WriteString(fmt.Sprintf("# Docs: %s\n", m.Meta.Docs))
	sb.WriteString("\n")

	for _, group := range m.Groups {
		dashes := strings.Repeat("─", dashWidth(group.Name))
		sb.WriteString(fmt.Sprintf("# ── %s %s\n", group.Name, dashes))
		sb.WriteString(fmt.Sprintf("# %s\n", group.Description))

		for _, v := range group.Vars {
			if v.Secret && !opts.IncludeSecrets {
				sb.WriteString(fmt.Sprintf(
					"# %s=  # SECRET — inject via SOPS or environment\n", v.Key,
				))
				continue
			}

			req := "[optional]"
			if v.Required {
				req = "[REQUIRED]"
			}
			desc := strings.ReplaceAll(strings.TrimSpace(v.Description), "\n", " ")
			sb.WriteString(fmt.Sprintf("# %s %s\n", req, desc))

			if len(v.Allowed) > 0 {
				sb.WriteString(fmt.Sprintf("# Allowed: %s\n", strings.Join(v.Allowed, ", ")))
			}
			if v.Example != "" {
				sb.WriteString(fmt.Sprintf("# Example: %s\n", v.Example))
			}

			sb.WriteString(fmt.Sprintf("%s=%s\n", v.Key, v.Default))
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
