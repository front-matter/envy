// Package writer renders compose.yaml into a .env file.
package writer

import (
	"fmt"
	"strings"

	"github.com/front-matter/envy/compose"
)

// Generate renders compose.yaml into a documented .env string.
func Generate(m *compose.Project) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s — Environment Configuration\n", m.Meta.Title))
	sb.WriteString("# Generated from compose.yaml — edit compose.yaml, not this file.\n")
	sb.WriteString(fmt.Sprintf("# Version: %s\n", m.Meta.VersionLabel()))
	sb.WriteString(fmt.Sprintf("# Docs: %s\n", m.Meta.Docs))
	sb.WriteString("\n")

	for _, set := range m.OrderedSets() {
		dashes := strings.Repeat("─", dashWidth(set.Key()))
		sb.WriteString(fmt.Sprintf("# ── %s %s\n", set.Key(), dashes))
		sb.WriteString(fmt.Sprintf("# %s\n", set.Description()))

		vars := set.Vars()
		for _, key := range compose.SortedVarKeys(vars) {
			sb.WriteString("#\n")
			sb.WriteString(fmt.Sprintf("%s=%s\n", key, compose.VarString(vars[key])))
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
