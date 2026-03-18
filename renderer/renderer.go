// Package renderer produces documentation from a manifest.
package renderer

import (
	"fmt"
	"strings"

	"github.com/front-matter/envy/manifest"
)

// Render dispatches to the correct format renderer.
func Render(m *manifest.Manifest, format string) (string, error) {
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

func renderMarkdown(m *manifest.Manifest) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s — Environment Variable Reference\n\n", m.Meta.Name))
	sb.WriteString(fmt.Sprintf(
		"> %s · [Upstream docs](%s)\n\n",
		m.Meta.VersionLabel(), m.Meta.Docs,
	))

	for _, group := range m.OrderedGroups() {
		sb.WriteString(fmt.Sprintf("## %s\n\n", group.Key))
		sb.WriteString(fmt.Sprintf("%s\n\n", group.Description))
		sb.WriteString("| Variable | Required | Default | Description |\n")
		sb.WriteString("|---|---|---|---|\n")

		for _, v := range group.Vars {
			req := "—"
			if v.Required {
				req = "✅"
			}
			secret := ""
			if v.Secret {
				secret = " 🔒"
			}
			defaultVal := "—"
			if v.Default.String() != "" {
				defaultVal = fmt.Sprintf("`%s`", v.Default.String())
			}
			desc := strings.ReplaceAll(strings.TrimSpace(v.Description), "\n", " ")
			sb.WriteString(fmt.Sprintf(
				"| `%s`%s | %s | %s | %s |\n",
				v.Key, secret, req, defaultVal, desc,
			))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ── RST ───────────────────────────────────────────────────────────────────────

func renderRST(m *manifest.Manifest) string {
	var sb strings.Builder

	title := fmt.Sprintf("%s Environment Variables", m.Meta.Name)
	sb.WriteString(title + "\n")
	sb.WriteString(strings.Repeat("=", len(title)) + "\n\n")

	for _, group := range m.OrderedGroups() {
		sb.WriteString(group.Key + "\n")
		sb.WriteString(strings.Repeat("-", len(group.Key)) + "\n\n")
		sb.WriteString(group.Description + "\n\n")

		for _, v := range group.Vars {
			req := ""
			if v.Required {
				req = " *(required)*"
			}
			sb.WriteString(fmt.Sprintf(".. envvar:: %s%s\n\n", v.Key, req))
			desc := strings.TrimSpace(v.Description)
			for _, line := range strings.Split(desc, "\n") {
				sb.WriteString(fmt.Sprintf("   %s\n", line))
			}
			if v.Default.String() != "" {
				sb.WriteString(fmt.Sprintf("\n   **Default:** ``%s``\n", v.Default.String()))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// ── Table ─────────────────────────────────────────────────────────────────────

func renderTable(m *manifest.Manifest) string {
	var sb strings.Builder
	header := fmt.Sprintf("%-60s %-4s %-40s %s", "VARIABLE", "REQ", "DEFAULT", "DESCRIPTION")
	sb.WriteString(header + "\n")
	sb.WriteString(strings.Repeat("─", len(header)) + "\n")

	for _, group := range m.OrderedGroups() {
		sb.WriteString(fmt.Sprintf("\n# %s\n", group.Key))
		for _, v := range group.Vars {
			req := "opt"
			if v.Required {
				req = "REQ"
			}
			desc := strings.ReplaceAll(strings.TrimSpace(v.Description), "\n", " ")
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			defaultVal := v.Default.String()
			if len(defaultVal) > 38 {
				defaultVal = defaultVal[:35] + "..."
			}
			sb.WriteString(fmt.Sprintf(
				"%-60s %-4s %-40s %s\n",
				v.Key, req, defaultVal, desc,
			))
		}
	}

	return sb.String()
}
