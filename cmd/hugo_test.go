package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/front-matter/envy/manifest"
)

func TestPrepareBuildContentDirCopiesExistingContentAndGeneratesGroupPages(t *testing.T) {
	siteRoot := t.TempDir()
	existingContentDir := filepath.Join(siteRoot, "content")
	if err := os.MkdirAll(existingContentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(content): %v", err)
	}
	if err := os.WriteFile(filepath.Join(existingContentDir, "about.md"), []byte("# About\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(about.md): %v", err)
	}

	m := &manifest.Manifest{
		Meta:     manifest.Meta{Title: "Example", Version: "v1"},
		Services: []manifest.Service{{Name: "web", Groups: []string{"common"}}},
		Groups: map[string]manifest.Group{
			"common": {
				Description: "Shared settings for runtime services.",
				Link:        "https://example.org/common",
				Vars:        []manifest.Var{{Key: "APP_ENV", Default: "production", Example: "staging"}},
			},
		},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	aboutContent, err := os.ReadFile(filepath.Join(contentDir, "about.md"))
	if err != nil {
		t.Fatalf("ReadFile(about.md): %v", err)
	}
	if string(aboutContent) != "# About\n" {
		t.Fatalf("expected copied content file, got %q", string(aboutContent))
	}

	indexContent, err := os.ReadFile(filepath.Join(contentDir, "groups", "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(groups/_index.md): %v", err)
	}
	if !strings.Contains(string(indexContent), "title=\"common\"") {
		t.Fatalf("expected generated groups index to render a card for common, got:\n%s", string(indexContent))
	}
	if !strings.Contains(string(indexContent), "name: Groups") {
		t.Fatalf("expected generated groups index to include menu metadata, got:\n%s", string(indexContent))
	}
	if !strings.Contains(string(indexContent), "{{< cards cols=\"3\" >}}") {
		t.Fatalf("expected generated groups index to include cards shortcode, got:\n%s", string(indexContent))
	}

	homeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.md): %v", err)
	}
	homeChecks := []string{
		"# Example",
		"## Overview",
		"## Navigation",
		"[Browse all groups](/groups/)",
		"{{< cards cols=\"2\" >}}",
		"{{< cards cols=\"3\" >}}",
		"title=\"Browse all groups\"",
		"title=\"common\"",
		"/groups/common/",
	}
	for _, check := range homeChecks {
		if !strings.Contains(string(homeContent), check) {
			t.Fatalf("expected generated home page to contain %q, got:\n%s", check, string(homeContent))
		}
	}

	groupContent, err := os.ReadFile(filepath.Join(contentDir, "groups", "common.md"))
	if err != nil {
		t.Fatalf("ReadFile(groups/common.md): %v", err)
	}
	checks := []string{
		"# common",
		"- [Home](/)",
		"- [All groups](/groups/)",
		"title=\"Back to groups\"",
		"title=\"External documentation\"",
		"Shared settings for runtime services.",
		"## Services",
		"- web",
		"## Documentation",
		"https://example.org/common",
		"## Variables",
		"title=\"APP_ENV\"",
		"link=\"#app_env\"",
		"### APP_ENV",
	}
	for _, check := range checks {
		if !strings.Contains(string(groupContent), check) {
			t.Fatalf("expected generated group page to contain %q, got:\n%s", check, string(groupContent))
		}
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestPrepareBuildContentDirKeepsExistingGroupPage(t *testing.T) {
	siteRoot := t.TempDir()
	groupDir := filepath.Join(siteRoot, "content", "groups")
	if err := os.MkdirAll(groupDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(groups): %v", err)
	}
	customPage := "# Custom\n"
	if err := os.WriteFile(filepath.Join(groupDir, "common.md"), []byte(customPage), 0o644); err != nil {
		t.Fatalf("WriteFile(common.md): %v", err)
	}

	m := &manifest.Manifest{
		Groups: map[string]manifest.Group{
			"common": {Description: "Shared settings."},
		},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	groupContent, err := os.ReadFile(filepath.Join(contentDir, "groups", "common.md"))
	if err != nil {
		t.Fatalf("ReadFile(groups/common.md): %v", err)
	}
	if string(groupContent) != customPage {
		t.Fatalf("expected existing group page to be preserved, got:\n%s", string(groupContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}
