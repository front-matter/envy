package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/front-matter/envy/manifest"
	"gopkg.in/yaml.v3"
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
		Meta:     manifest.Meta{Title: "Example", Description: "Example description", Version: "v1"},
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
		"Example description",
	}
	for _, check := range homeChecks {
		if !strings.Contains(string(homeContent), check) {
			t.Fatalf("expected generated home page to contain %q, got:\n%s", check, string(homeContent))
		}
	}

	homeNotChecks := []string{
		"## Overview",
		"## Navigation",
		"[Browse all groups](/groups/)",
		"{{< cards cols=\"2\" >}}",
		"{{< cards cols=\"3\" >}}",
		"title=\"Browse all groups\"",
		"title=\"common\"",
		"/groups/common/",
	}
	for _, check := range homeNotChecks {
		if strings.Contains(string(homeContent), check) {
			t.Fatalf("expected generated home page to not contain %q, got:\n%s", check, string(homeContent))
		}
	}

	groupContent, err := os.ReadFile(filepath.Join(contentDir, "groups", "common.md"))
	if err != nil {
		t.Fatalf("ReadFile(groups/common.md): %v", err)
	}
	checks := []string{
		"Shared settings for runtime services.",
		"## Services",
		"- web",
		"## Documentation",
		"https://example.org/common",
		"## Variables",
		"title=\"APP_ENV\"",
		"link=\"#app_env\"",
		`<div id="app_env">`,
	}
	for _, check := range checks {
		if !strings.Contains(string(groupContent), check) {
			t.Fatalf("expected generated group page to contain %q, got:\n%s", check, string(groupContent))
		}
	}

	servicesIndexContent, err := os.ReadFile(filepath.Join(contentDir, "services", "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(services/_index.md): %v", err)
	}
	servicesChecks := []string{
		"title: Services",
		"name: Services",
		"title=\"web\"",
	}
	for _, check := range servicesChecks {
		if !strings.Contains(string(servicesIndexContent), check) {
			t.Fatalf("expected generated services index to contain %q, got:\n%s", check, string(servicesIndexContent))
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

func TestPrepareBuildContentDirUsesDocsIndexAsHome(t *testing.T) {
	siteRoot := t.TempDir()
	docsDir := filepath.Join(siteRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(docs): %v", err)
	}
	readmeHome := "# README Home\n"
	if err := os.WriteFile(filepath.Join(siteRoot, "README.md"), []byte(readmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md): %v", err)
	}
	customHome := "# Custom Docs Home\n"
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte(customHome), 0o644); err != nil {
		t.Fatalf("WriteFile(docs/index.md): %v", err)
	}

	m := &manifest.Manifest{
		Meta: manifest.Meta{Title: "Example"},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	homeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.md): %v", err)
	}
	if string(homeContent) != customHome {
		t.Fatalf("expected docs/index.md to override generated home page, got:\n%s", string(homeContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestPrepareBuildContentDirUsesReadmeAsHome(t *testing.T) {
	siteRoot := t.TempDir()
	readmeHome := "# README Home\n"
	if err := os.WriteFile(filepath.Join(siteRoot, "README.md"), []byte(readmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md): %v", err)
	}

	m := &manifest.Manifest{
		Meta: manifest.Meta{Title: "Example"},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	homeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.md): %v", err)
	}
	if string(homeContent) != readmeHome {
		t.Fatalf("expected README.md to be used as home page, got:\n%s", string(homeContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestUsesGeneratedHugoSite(t *testing.T) {
	tests := []struct {
		subcommand string
		want       bool
	}{
		{subcommand: "build", want: true},
		{subcommand: "server", want: true},
		{subcommand: "deploy", want: true},
		{subcommand: "version", want: false},
	}

	for _, tt := range tests {
		if got := usesGeneratedHugoSite(tt.subcommand); got != tt.want {
			t.Fatalf("usesGeneratedHugoSite(%q) = %v, want %v", tt.subcommand, got, tt.want)
		}
	}
}

func TestWriteTempHugoConfigFromManifestIncludesMetaIgnoreLogs(t *testing.T) {
	siteDir := t.TempDir()
	m := &manifest.Manifest{
		Meta: manifest.Meta{
			Title:      "Example",
			IgnoreLogs: []string{"warning-goldmark-raw-html"},
		},
	}

	if err := writeTempHugoConfigFromManifest(m, siteDir); err != nil {
		t.Fatalf("writeTempHugoConfigFromManifest(): %v", err)
	}

	configBytes, err := os.ReadFile(filepath.Join(siteDir, "hugo.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(hugo.yaml): %v", err)
	}

	var got map[string]interface{}
	if err := yaml.Unmarshal(configBytes, &got); err != nil {
		t.Fatalf("yaml.Unmarshal(hugo.yaml): %v", err)
	}

	ignoreLogs, ok := got["ignoreLogs"].([]interface{})
	if !ok || len(ignoreLogs) != 1 || ignoreLogs[0] != "warning-goldmark-raw-html" {
		t.Fatalf("expected ignoreLogs [warning-goldmark-raw-html], got: %#v", got["ignoreLogs"])
	}

	module, ok := got["module"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected module map in hugo config, got: %#v", got["module"])
	}
	imports, ok := module["imports"].([]interface{})
	if !ok || len(imports) == 0 {
		t.Fatalf("expected non-empty module.imports, got: %#v", module["imports"])
	}

	menu, ok := got["menu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected menu map in hugo config, got: %#v", got["menu"])
	}
	mainMenu, ok := menu["main"].([]interface{})
	if !ok || len(mainMenu) < 2 {
		t.Fatalf("expected non-empty menu.main, got: %#v", menu["main"])
	}

	firstMenuItem, ok := mainMenu[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first menu item to be map, got: %#v", mainMenu[0])
	}
	if firstMenuItem["name"] != "Search" {
		t.Fatalf("expected first config menu item to be Search, got: %#v", firstMenuItem["name"])
	}
	params, ok := firstMenuItem["params"].(map[string]interface{})
	if !ok || params["type"] != "search" {
		t.Fatalf("expected first menu item params.type to be search, got: %#v", firstMenuItem["params"])
	}

	secondMenuItem, ok := mainMenu[1].(map[string]interface{})
	if !ok {
		t.Fatalf("expected second menu item to be map, got: %#v", mainMenu[1])
	}
	if secondMenuItem["name"] != "Theme" {
		t.Fatalf("expected second config menu item to be Theme, got: %#v", secondMenuItem["name"])
	}
	themeParams, ok := secondMenuItem["params"].(map[string]interface{})
	if !ok || themeParams["type"] != "theme-toggle" {
		t.Fatalf("expected second menu item params.type to be theme-toggle, got: %#v", secondMenuItem["params"])
	}
}
