// Package renderer produces documentation from a compose.
package renderer

import (
	"fmt"
	"strings"

	"github.com/front-matter/envy/compose"
)

// Render dispatches to the correct format renderer.
func Render(m *compose.Project, format string) (string, error) {
	switch format {
	case "markdown", "md", "":
		return renderMarkdown(m), nil
	case "rst":
		return renderRST(m), nil
	case "table":
		return renderTable(m), nil
	default:
		return "", fmt.Errorf("unknown format %q — use markdown, rst, or table", format)
	}
}

// ── Markdown ──────────────────────────────────────────────────────────────────

func renderMarkdown(m *compose.Project) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s — Environment Variable Reference\n\n", m.Meta.Title))
	sb.WriteString(fmt.Sprintf(
		"> %s · %s\n\n",
		m.Meta.VersionLabel(), m.Meta.Docs,
	))

	for _, set := range m.OrderedSets() {
		sb.WriteString(fmt.Sprintf("## %s\n\n", set.Key()))
		sb.WriteString(fmt.Sprintf("%s\n\n", set.Description()))
		sb.WriteString("| Variable | Default | Description |\n")
		sb.WriteString("|---|---|---|\n")

		for _, v := range set.Vars() {
			defaultVal := "—"
			if v.DefaultString() != "" {
				defaultVal = fmt.Sprintf("`%s`", v.DefaultString())
			}
			desc := strings.ReplaceAll(strings.TrimSpace(v.Description), "\n", " ")
			sb.WriteString(fmt.Sprintf(
				"| `%s` | %s | %s |\n",
				v.Key, defaultVal, desc,
			))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ── RST ───────────────────────────────────────────────────────────────────────

func renderRST(m *compose.Project) string {
	var sb strings.Builder

	title := fmt.Sprintf("%s Environment Variables", m.Meta.Title)
	sb.WriteString(title + "\n")
	sb.WriteString(strings.Repeat("=", len(title)) + "\n\n")

	for _, set := range m.OrderedSets() {
		sb.WriteString(set.Key() + "\n")
		sb.WriteString(strings.Repeat("-", len(set.Key())) + "\n\n")
		sb.WriteString(set.Description() + "\n\n")

		for _, v := range set.Vars() {
			sb.WriteString(fmt.Sprintf(".. envvar:: %s\n\n", v.Key))
			desc := strings.TrimSpace(v.Description)
			for _, line := range strings.Split(desc, "\n") {
				sb.WriteString(fmt.Sprintf("   %s\n", line))
			}
			if v.DefaultString() != "" {
				sb.WriteString(fmt.Sprintf("\n   **Default:** ``%s``\n", v.DefaultString()))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// ── Table ─────────────────────────────────────────────────────────────────────

func renderTable(m *compose.Project) string {
	var sb strings.Builder
	header := fmt.Sprintf("%-60s %-4s %-40s %s", "VARIABLE", "REQ", "DEFAULT", "DESCRIPTION")
	sb.WriteString(header + "\n")
	sb.WriteString(strings.Repeat("─", len(header)) + "\n")

	for _, set := range m.OrderedSets() {
		sb.WriteString(fmt.Sprintf("\n# %s\n", set.Key()))
		for _, v := range set.Vars() {
			desc := strings.ReplaceAll(strings.TrimSpace(v.Description), "\n", " ")
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			defaultVal := v.DefaultString()
			if len(defaultVal) > 38 {
				defaultVal = defaultVal[:35] + "..."
			}
			sb.WriteString(fmt.Sprintf(
				"%-60s %-40s %s\n",
				v.Key, defaultVal, desc,
			))
		}
	}

	return sb.String()
}
