package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/front-matter/envy/compose"
	"gopkg.in/yaml.v3"
)

func TestNormalizeSetDocLink(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLabel  string
		wantTarget string
	}{
		{
			name:       "plain url",
			input:      "https://example.org/common",
			wantLabel:  "https://example.org/common",
			wantTarget: "https://example.org/common",
		},
		{
			name:       "markdown reference style",
			input:      "[Common Docs]: https://example.org/common",
			wantLabel:  "Common Docs",
			wantTarget: "https://example.org/common",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, target := normalizeSetDocLink(tt.input)
			if label != tt.wantLabel || target != tt.wantTarget {
				t.Fatalf("normalizeSetDocLink(%q) = (%q, %q), want (%q, %q)", tt.input, label, target, tt.wantLabel, tt.wantTarget)
			}
		})
	}
}

func TestPrepareBuildContentDirCopiesExistingContentAndGeneratesGroupPages(t *testing.T) {
	siteRoot := t.TempDir()
	existingContentDir := filepath.Join(siteRoot, "content")
	if err := os.MkdirAll(existingContentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(content): %v", err)
	}
	if err := os.WriteFile(filepath.Join(existingContentDir, "about.md"), []byte("# About\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(about.md): %v", err)
	}

	m := &compose.Project{
		Meta: compose.Meta{
			Title:               "Example",
			Description:         "Example description",
			Version:             "v1",
			HugoDefaultLanguage: "en",
			HugoLanguages:       "en:\n  languageName: English\n  weight: 1\nde:\n  languageName: Deutsch\n  weight: 2\n",
		},
		Services: []compose.Service{{
			Name:     "web",
			Image:    "caddy:2.10",
			Platform: "linux/amd64",
			Command:  []string{"caddy", "run", "--config", "/etc/caddy/Caddyfile"},
			Sets:     []string{"common"},
		}},
		Sets: map[string]compose.Set{
			"common": {
				Description: "Shared settings for runtime services.",
				Link:        "[Common Docs]: https://example.org/common",
				Vars: []compose.Var{
					{Key: "APP_ENV", Default: "production", Required: "true", Example: "staging"},
					{Key: "TEST_READONLY_VAR", Default: "locked-value", Readonly: "true"},
				},
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

	indexContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/_index.md): %v", err)
	}
	if !strings.Contains(string(indexContent), "title=\"common\"") {
		t.Fatalf("expected generated sets index to render a card for common, got:\n%s", string(indexContent))
	}
	if !strings.Contains(string(indexContent), "iconImage=\"/images/properties.svg\"") {
		t.Fatalf("expected generated sets index to render properties.svg icon, got:\n%s", string(indexContent))
	}
	indexChecks := []string{
		"titleLink=\"/sets/common/\"",
		"iconImageClass=\"hx:h-8 hx:w-8 md:h-10 md:w-10 hx:shrink-0\"",
		"subtitle=`Shared settings for runtime services.`",
		"subtitle2=`<a href=\"https://example.org/common\" class=\"inline-flex items-center gap-2\"><img src=\"/images/readme.svg\" class=\"h-4 w-4\" /><span>Common Docs</span></a>`",
		`tags="web"`,
		`tagLinks="/services/#web"`,
		`tagColor="red"`,
		`tagBorder="false"`,
	}
	for _, check := range indexChecks {
		if !strings.Contains(string(indexContent), check) {
			t.Fatalf("expected generated sets index to contain %q, got:\n%s", check, string(indexContent))
		}
	}
	if !strings.Contains(string(indexContent), "name: Sets") {
		t.Fatalf("expected generated sets index to include menu metadata, got:\n%s", string(indexContent))
	}
	if !strings.Contains(string(indexContent), "{{< cards cols=\"2\" >}}") {
		t.Fatalf("expected generated sets index to include cards shortcode, got:\n%s", string(indexContent))
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
		"[Browse all sets](/sets/)",
		"{{< cards cols=\"2\" >}}",
		"{{< cards cols=\"3\" >}}",
		"title=\"Browse all sets\"",
		"title=\"common\"",
		"/sets/common/",
	}
	for _, check := range homeNotChecks {
		if strings.Contains(string(homeContent), check) {
			t.Fatalf("expected generated home page to not contain %q, got:\n%s", check, string(homeContent))
		}
	}

	groupContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "common.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/common.md): %v", err)
	}
	checks := []string{
		"Shared settings for runtime services.",
		"https://example.org/common",
		"title=\"APP_ENV\"",
		`<div id="app_env">`,
		"title=\"common\"",
		"iconImage=\"/images/properties.svg\"",
		"iconImageClass=\"hx:h-8 hx:w-8 md:h-10 md:w-10 hx:shrink-0\"",
		"titleClass=\"hx:text-4xl md:hx:text-5xl hx:tracking-tight hx:pr-40 md:hx:pr-56\"",
		"toc: false",
		"subtitle=`Shared settings for runtime services.`",
		"subtitle2=`<a href=\"https://example.org/common\" class=\"inline-flex items-center gap-2\"><img src=\"/images/readme.svg\" class=\"h-4 w-4\" /><span>Common Docs</span></a>`",
		`tags="web"`,
		`tagLinks="/services/#web"`,
		`tagColor="red"`,
		`tagBorder="false"`,
		`titlePadding="hx:py-4 hx:px-4"`,
		`data-editable="true"`,
		`hx:mb-4`,
		`hx:border-blue-200 hx:bg-blue-50/70`,
		`contenteditable="true" spellcheck="false">production</code>`,
		`data-editable="false"`,
		`hx:border-yellow-200 hx:bg-yellow-50/80`,
		`contenteditable="false" spellcheck="false">locked-value</code>`,
	}
	for _, check := range checks {
		if !strings.Contains(string(groupContent), check) {
			t.Fatalf("expected generated set page to contain %q, got:\n%s", check, string(groupContent))
		}
	}
	if strings.Contains(string(groupContent), "link=\"#app_env\"") {
		t.Fatalf("expected generated set page variable cards to be non-clickable, got:\n%s", string(groupContent))
	}

	localizedSetsIndexContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/_index.de.md): %v", err)
	}
	localizedSetsChecks := []string{
		"title: Sets",
		"description: Automatisch generierte Referenz der Konfigurations-Sets aus compose.yml.",
		"name: Sets",
	}
	for _, check := range localizedSetsChecks {
		if !strings.Contains(string(localizedSetsIndexContent), check) {
			t.Fatalf("expected generated localized sets index to contain %q, got:\n%s", check, string(localizedSetsIndexContent))
		}
	}

	localizedGroupContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "common.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/common.de.md): %v", err)
	}
	localizedGroupChecks := []string{
		"Erforderlich",
		"Beispiel: 'staging'",
		`tag="Schreibgeschuetzt"`,
	}
	for _, check := range localizedGroupChecks {
		if !strings.Contains(string(localizedGroupContent), check) {
			t.Fatalf("expected generated localized set page to contain %q, got:\n%s", check, string(localizedGroupContent))
		}
	}

	servicesIndexContent, err := os.ReadFile(filepath.Join(contentDir, "services", "_index.md"))
	if err != nil {
		t.Fatalf("ReadFile(services/_index.md): %v", err)
	}
	servicesChecks := []string{
		"title: Services",
		"hide: true",
		"name: Services",
		"title=\"web\"",
		"titleLink=\"#web\"",
		"titleClass=\"hx:pr-32 md:hx:pr-40\"",
		"subtitle2=`**Image:** [caddy:2.10](https://hub.docker.com/_/caddy)`",
		"subtitle3=`**Platform:** linux/amd64`",
		"subtitle4=`**Command:**",
		"caddy run --config /etc/caddy/Caddyfile",
		`tags="common"`,
		`tagLinks="/sets/common/"`,
		`tagColor="blue"`,
		`tagBorder="false"`,
	}
	for _, check := range servicesChecks {
		if !strings.Contains(string(servicesIndexContent), check) {
			t.Fatalf("expected generated services index to contain %q, got:\n%s", check, string(servicesIndexContent))
		}
	}

	localizedServicesIndexContent, err := os.ReadFile(filepath.Join(contentDir, "services", "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(services/_index.de.md): %v", err)
	}
	localizedServicesChecks := []string{
		"title: Dienste",
		"name: Dienste",
		"subtitle3=`**Plattform:** linux/amd64`",
		"subtitle4=`**Befehl:**",
	}
	for _, check := range localizedServicesChecks {
		if !strings.Contains(string(localizedServicesIndexContent), check) {
			t.Fatalf("expected generated localized services index to contain %q, got:\n%s", check, string(localizedServicesIndexContent))
		}
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}
func TestPrepareBuildContentDirKeepsExistingGroupPage(t *testing.T) {
	siteRoot := t.TempDir()
	groupDir := filepath.Join(siteRoot, "content", "sets")
	if err := os.MkdirAll(groupDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(sets): %v", err)
	}
	customPage := "# Custom\n"
	if err := os.WriteFile(filepath.Join(groupDir, "common.md"), []byte(customPage), 0o644); err != nil {
		t.Fatalf("WriteFile(common.md): %v", err)
	}

	m := &compose.Project{
		Sets: map[string]compose.Set{
			"common": {Description: "Shared settings."},
		},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	groupContent, err := os.ReadFile(filepath.Join(contentDir, "sets", "common.md"))
	if err != nil {
		t.Fatalf("ReadFile(sets/common.md): %v", err)
	}
	if string(groupContent) != customPage {
		t.Fatalf("expected existing set page to be preserved, got:\n%s", string(groupContent))
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

	m := &compose.Project{
		Meta: compose.Meta{Title: "Example"},
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
		t.Fatalf("expected README.md to be used for home page, got:\n%s", string(homeContent))
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

	m := &compose.Project{
		Meta: compose.Meta{Title: "Example"},
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

func TestPrepareBuildContentDirUsesLocalizedReadmeAsHome(t *testing.T) {
	siteRoot := t.TempDir()
	readmeHome := "# README Home\n"
	localizedReadmeHome := "# README Startseite\n"
	if err := os.WriteFile(filepath.Join(siteRoot, "README.md"), []byte(readmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md): %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteRoot, "README.de.md"), []byte(localizedReadmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.de.md): %v", err)
	}

	m := &compose.Project{
		Meta: compose.Meta{
			Title:               "Example",
			HugoDefaultLanguage: "en",
			HugoLanguages:       "en:\n  languageName: English\n  weight: 1\nde:\n  languageName: Deutsch\n  weight: 2\n",
		},
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
		t.Fatalf("expected README.md to be used as default home page, got:\n%s", string(homeContent))
	}

	localizedHomeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.de.md): %v", err)
	}
	if string(localizedHomeContent) != localizedReadmeHome {
		t.Fatalf("expected README.de.md to be used as german home page, got:\n%s", string(localizedHomeContent))
	}

	if err := os.RemoveAll(contentDir); err != nil {
		t.Fatalf("RemoveAll(contentDir): %v", err)
	}
}

func TestPrepareBuildContentDirFallsBackToDefaultReadmeForLocalizedHome(t *testing.T) {
	siteRoot := t.TempDir()
	readmeHome := "# README Home\n"
	if err := os.WriteFile(filepath.Join(siteRoot, "README.md"), []byte(readmeHome), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md): %v", err)
	}

	m := &compose.Project{
		Meta: compose.Meta{
			Title:               "Example",
			HugoDefaultLanguage: "en",
			HugoLanguages:       "en:\n  languageName: English\n  weight: 1\nde:\n  languageName: Deutsch\n  weight: 2\n",
		},
	}

	contentDir, err := prepareBuildContentDir(siteRoot, m)
	if err != nil {
		t.Fatalf("prepareBuildContentDir(): %v", err)
	}

	localizedHomeContent, err := os.ReadFile(filepath.Join(contentDir, "_index.de.md"))
	if err != nil {
		t.Fatalf("ReadFile(_index.de.md): %v", err)
	}
	if string(localizedHomeContent) != readmeHome {
		t.Fatalf("expected README.md fallback for german home page, got:\n%s", string(localizedHomeContent))
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
	m := &compose.Project{
		Meta: compose.Meta{
			Title:                    "Legacy Example",
			IgnoreLogs:               []string{"legacy-ignore"},
			HugoTitle:                "Hugo Example",
			HugoParamsDescription:    "Configured via HUGO_PARAMS_DESCRIPTION",
			HugoIgnoreLogs:           []string{"warning-goldmark-raw-html"},
			MarkupGoldmarkUnsafe:     "false",
			HugoMarkupGoldmarkUnsafe: "true",
		},
	}

	if err := writeTempHugoConfigFromManifest(m, siteDir, "https://github.com/front-matter/envy"); err != nil {
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

	if got["title"] != "Hugo Example" {
		t.Fatalf("expected title Hugo Example, got: %#v", got["title"])
	}

	paramsConfig, ok := got["params"].(map[string]interface{})
	if !ok || paramsConfig["description"] != "Configured via HUGO_PARAMS_DESCRIPTION" {
		t.Fatalf("expected params.description from HUGO_PARAMS_DESCRIPTION, got: %#v", got["params"])
	}

	markup, ok := got["markup"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected markup map, got: %#v", got["markup"])
	}
	goldmark, ok := markup["goldmark"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected markup.goldmark map, got: %#v", markup["goldmark"])
	}
	renderer, ok := goldmark["renderer"].(map[string]interface{})
	if !ok || renderer["unsafe"] != true {
		t.Fatalf("expected markup.goldmark.renderer.unsafe=true, got: %#v", goldmark["renderer"])
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
	if !ok || len(mainMenu) < 3 {
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

	thirdMenuItem, ok := mainMenu[2].(map[string]interface{})
	if !ok {
		t.Fatalf("expected third menu item to be map, got: %#v", mainMenu[2])
	}
	if thirdMenuItem["name"] != "GitHub" {
		t.Fatalf("expected third config menu item to be GitHub, got: %#v", thirdMenuItem["name"])
	}
	if thirdMenuItem["url"] != "https://github.com/front-matter/envy" {
		t.Fatalf("expected third menu item url to be repo URL, got: %#v", thirdMenuItem["url"])
	}
	githubParams, ok := thirdMenuItem["params"].(map[string]interface{})
	if !ok || githubParams["icon"] != "github" {
		t.Fatalf("expected third menu item params.icon to be github, got: %#v", thirdMenuItem["params"])
	}
}

func TestNormalizeRepositoryURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "https", in: "https://github.com/front-matter/envy.git", want: "https://github.com/front-matter/envy"},
		{name: "http", in: "http://github.com/front-matter/envy.git", want: "http://github.com/front-matter/envy"},
		{name: "scp ssh", in: "git@github.com:front-matter/envy.git", want: "https://github.com/front-matter/envy"},
		{name: "ssh url", in: "ssh://git@github.com/front-matter/envy.git", want: "https://github.com/front-matter/envy"},
		{name: "unsupported", in: "file:///tmp/repo", want: ""},
		{name: "empty", in: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeRepositoryURL(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeRepositoryURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestWriteTempHugoConfigFromManifestIncludesMultilanguage(t *testing.T) {
	siteDir := t.TempDir()
	m := &compose.Project{
		Meta: compose.Meta{
			Title:               "Example",
			HugoDefaultLanguage: "en",
			HugoDefaultInSubdir: "true",
			HugoLanguages:       "en:\n  languageName: English\n  weight: 1\nde:\n  languageName: Deutsch\n  weight: 2\n",
		},
	}

	if err := writeTempHugoConfigFromManifest(m, siteDir, ""); err != nil {
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

	if got["defaultContentLanguage"] != "en" {
		t.Fatalf("expected defaultContentLanguage en, got: %#v", got["defaultContentLanguage"])
	}
	if got["defaultContentLanguageInSubdir"] != true {
		t.Fatalf("expected defaultContentLanguageInSubdir true, got: %#v", got["defaultContentLanguageInSubdir"])
	}

	languages, ok := got["languages"].(map[string]interface{})
	if !ok || len(languages) != 2 {
		t.Fatalf("expected languages map with 2 entries, got: %#v", got["languages"])
	}
	if _, ok := languages["en"]; !ok {
		t.Fatalf("expected languages.en to exist, got: %#v", languages)
	}
	if _, ok := languages["de"]; !ok {
		t.Fatalf("expected languages.de to exist, got: %#v", languages)
	}

	menu, ok := got["menu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected menu map in hugo config, got: %#v", got["menu"])
	}
	mainMenu, ok := menu["main"].([]interface{})
	if !ok || len(mainMenu) < 3 {
		t.Fatalf("expected menu.main with language switch, got: %#v", menu["main"])
	}

	foundLanguageSwitch := false
	for _, item := range mainMenu {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		params, ok := entry["params"].(map[string]interface{})
		if !ok {
			continue
		}
		if params["type"] == "language-switch" {
			foundLanguageSwitch = true
			break
		}
	}
	if !foundLanguageSwitch {
		t.Fatalf("expected language-switch menu item in menu.main, got: %#v", menu["main"])
	}

	deConfig, ok := languages["de"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected languages.de to be map, got: %#v", languages["de"])
	}
	deMenu, ok := deConfig["menu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected languages.de.menu map, got: %#v", deConfig["menu"])
	}
	deMainMenu, ok := deMenu["main"].([]interface{})
	if !ok || len(deMainMenu) < 3 {
		t.Fatalf("expected localized languages.de.menu.main, got: %#v", deMenu["main"])
	}

	deFirstMenuItem, ok := deMainMenu[0].(map[string]interface{})
	if !ok || deFirstMenuItem["name"] != "Suche" {
		t.Fatalf("expected first german menu item to be Suche, got: %#v", deMainMenu[0])
	}
	deSecondMenuItem, ok := deMainMenu[1].(map[string]interface{})
	if !ok || deSecondMenuItem["name"] != "Design" {
		t.Fatalf("expected second german menu item to be Design, got: %#v", deMainMenu[1])
	}
	deThirdMenuItem, ok := deMainMenu[2].(map[string]interface{})
	if !ok || deThirdMenuItem["name"] != "Sprache" {
		t.Fatalf("expected third german menu item to be Sprache, got: %#v", deMainMenu[2])
	}
}
