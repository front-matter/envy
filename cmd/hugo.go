package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/front-matter/envy/manifest"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var buildCmd = &cobra.Command{
	Use:                "build [hugo flags]",
	Short:              "Forward to hugo build",
	Long:               "Run hugo build with env.yaml as configuration source (instead of hugo.yaml).",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHugoCommand("build", args)
	},
}

var serverCmd = &cobra.Command{
	Use:                "server [hugo flags]",
	Short:              "Forward to hugo server",
	Long:               "Run hugo server with the same flags and arguments as Hugo.",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHugoCommand("server", args)
	},
}

var deployCmd = &cobra.Command{
	Use:                "deploy [hugo flags]",
	Short:              "Forward to hugo deploy",
	Long:               "Run hugo deploy with the same flags and arguments as Hugo.",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHugoCommand("deploy", args)
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(deployCmd)
}

func runHugoCommand(subcommand string, args []string) error {
	hugoPath, err := exec.LookPath("hugo")
	if err != nil {
		return fmt.Errorf("hugo executable not found in PATH: %w", err)
	}

	buildSiteDir := ""
	if usesGeneratedHugoSite(subcommand) {
		if hasConfigFlag(args) {
			return fmt.Errorf("envy %s uses env.yaml as Hugo config source; remove --config/-c", subcommand)
		}

		manifestFilePath, err := resolveBuildManifestPath()
		if err != nil {
			return err
		}

		buildSiteDir, err = prepareBuildAssets(manifestFilePath)
		if err != nil {
			return err
		}
		defer os.RemoveAll(buildSiteDir)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determining working directory: %w", err)
	}

	allArgs := make([]string, 0, len(args)+3)
	allArgs = append(allArgs, subcommand)
	if subcommand == "build" {
		allArgs = append(allArgs, "--destination", filepath.Join(cwd, "public"))
	}
	allArgs = append(allArgs, args...)

	command := exec.Command(hugoPath, allArgs...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if usesGeneratedHugoSite(subcommand) {
		command.Dir = buildSiteDir
		// Force vendor mode and skip checksum DB so Hugo uses the embedded
		// _vendor/ tree without any network access.
		command.Env = append(os.Environ(),
			"GOFLAGS=-mod=vendor",
			"GONOSUMDB=*",
			"GONOSUMCHECK=*",
		)
	}

	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("hugo %s exited with code %d", subcommand, exitErr.ExitCode())
		}
		return fmt.Errorf("running hugo %s: %w", subcommand, err)
	}

	return nil
}

func usesGeneratedHugoSite(subcommand string) bool {
	return subcommand == "build" || subcommand == "server" || subcommand == "deploy"
}

func hasConfigFlag(args []string) bool {
	for i, arg := range args {
		if arg == "--config" || arg == "-c" {
			return true
		}
		if strings.HasPrefix(arg, "--config=") {
			return true
		}
		if strings.HasPrefix(arg, "-c=") {
			return true
		}
		if arg == "-c" && i+1 < len(args) {
			return true
		}
	}
	return false
}

func resolveBuildManifestPath() (string, error) {
	path, err := resolveManifest(manifestPath)
	if err != nil {
		return "", fmt.Errorf("envy build requires env.yaml: %w\n\nSuggested fields in env.yaml for Hugo:\n\nmeta:\n  title: My Hugo Site\n  docs: https://example.org/\n  languageCode: en-US\n\ngroups:\n  hugo:\n    description: Hugo site configuration\n    vars:\n      HUGO_DEFAULT_CONTENT_LANGUAGE:\n        default: \"en\"\n      HUGO_TITLE:\n        default: \"My Hugo Site\"", err)
	}

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("envy build requires env.yaml at %s\n\nSuggested fields in env.yaml for Hugo:\n\nmeta:\n  title: My Hugo Site\n  docs: https://example.org/\n  languageCode: en-US\n\ngroups:\n  hugo:\n    description: Hugo site configuration\n    vars:\n      HUGO_DEFAULT_CONTENT_LANGUAGE:\n        default: \"en\"\n      HUGO_TITLE:\n        default: \"My Hugo Site\"", path)
		}
		return "", fmt.Errorf("checking env.yaml at %s: %w", path, err)
	}

	return path, nil
}

func prepareBuildAssets(path string) (string, error) {
	m, err := manifest.Load(path)
	if err != nil {
		return "", err
	}

	manifestDir := filepath.Dir(path)

	siteDir, err := os.MkdirTemp("", "envy-hugo-site-*")
	if err != nil {
		return "", fmt.Errorf("creating temporary Hugo site directory: %w", err)
	}

	if err := extractDocsFS(siteDir); err != nil {
		os.RemoveAll(siteDir)
		return "", err
	}

	// Write the Hugo site go.mod (not embedded to avoid Go module boundary error).
	siteGoMod := "module github.com/front-matter/envy/docs\n\ngo 1.21\n\nrequire github.com/imfing/hextra v0.12.1\n"
	if err := os.WriteFile(filepath.Join(siteDir, "go.mod"), []byte(siteGoMod), 0o644); err != nil {
		os.RemoveAll(siteDir)
		return "", fmt.Errorf("writing Hugo site go.mod: %w", err)
	}

	contentDir, err := prepareBuildContentDir(manifestDir, m)
	if err != nil {
		os.RemoveAll(siteDir)
		return "", err
	}
	defer os.RemoveAll(contentDir)

	if err := copyDirIfExists(contentDir, filepath.Join(siteDir, "content")); err != nil {
		os.RemoveAll(siteDir)
		return "", err
	}

	if err := copyDirIfExists(filepath.Join(manifestDir, "data"), filepath.Join(siteDir, "data")); err != nil {
		os.RemoveAll(siteDir)
		return "", err
	}

	if err := writeTempHugoConfigFromManifest(m, siteDir); err != nil {
		os.RemoveAll(siteDir)
		return "", err
	}

	return siteDir, nil
}

func extractDocsFS(dst string) error {
	sub, err := fs.Sub(docsFS, "docs")
	if err != nil {
		return fmt.Errorf("accessing embedded docs: %w", err)
	}
	return fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		target := filepath.Join(dst, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := fs.ReadFile(sub, path)
		if readErr != nil {
			return fmt.Errorf("reading embedded %s: %w", path, readErr)
		}
		if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func writeTempHugoConfigFromManifest(m *manifest.Manifest, siteDir string) error {
	lookup := make(map[string]manifest.Var)
	for _, v := range m.AllVars() {
		lookup[v.Key] = v
	}

	config := map[string]interface{}{
		"module": map[string]interface{}{
			"imports": []map[string]string{{"path": "github.com/imfing/hextra"}},
		},
		"menu": map[string]interface{}{
			"main": []map[string]interface{}{
				{
					"name":   "Search",
					"weight": 100,
					"params": map[string]string{"type": "search"},
				},
				{
					"name":   "Theme",
					"weight": 110,
					"params": map[string]string{"type": "theme-toggle"},
				},
			},
		},
	}

	if m.Meta.Docs != "" {
		config["baseURL"] = m.Meta.Docs
	}
	config["languageCode"] = m.Meta.LanguageCodeLabel()
	if v, ok := lookup["HUGO_LANGUAGE_CODE"]; ok && strings.TrimSpace(v.DefaultString()) != "" {
		config["languageCode"] = v.DefaultString()
	}
	if v, ok := lookup["HUGO_DEFAULT_CONTENT_LANGUAGE"]; ok && strings.TrimSpace(v.DefaultString()) != "" {
		config["defaultContentLanguage"] = v.DefaultString()
	}
	if v, ok := lookup["HUGO_TITLE"]; ok && strings.TrimSpace(v.DefaultString()) != "" {
		config["title"] = v.DefaultString()
	} else if strings.TrimSpace(m.Meta.Title) != "" {
		config["title"] = m.Meta.Title
	}

	if len(m.Meta.IgnoreLogs) > 0 {
		config["ignoreLogs"] = m.Meta.IgnoreLogs
	}

	content, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("rendering Hugo config: %w", err)
	}

	if err := os.WriteFile(filepath.Join(siteDir, "hugo.yaml"), content, 0o644); err != nil {
		return fmt.Errorf("writing Hugo config: %w", err)
	}

	return nil
}

func prepareBuildContentDir(siteRoot string, m *manifest.Manifest) (string, error) {
	tmpDir, err := os.MkdirTemp("", "envy-hugo-content-*")
	if err != nil {
		return "", fmt.Errorf("creating temporary Hugo content directory: %w", err)
	}

	sourceContentDir := filepath.Join(siteRoot, "content")
	if err := copyDirIfExists(sourceContentDir, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	customDocsIndex := filepath.Join(siteRoot, "docs", "index.md")
	if err := copyFileIfExists(customDocsIndex, filepath.Join(tmpDir, "_index.md")); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	readmeHome := filepath.Join(siteRoot, "README.md")
	if err := copyFileIfMissing(readmeHome, filepath.Join(tmpDir, "_index.md")); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	groupsDir := filepath.Join(tmpDir, "groups")
	if err := os.MkdirAll(groupsDir, 0o755); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("creating generated groups directory: %w", err)
	}

	if err := writeFileIfMissing(filepath.Join(tmpDir, "_index.md"), generateHomeMarkdown(m)); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	if err := writeFileIfMissing(filepath.Join(groupsDir, "_index.md"), generateGroupsIndexMarkdown(m)); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	for _, group := range m.OrderedGroups() {
		pagePath := filepath.Join(groupsDir, sanitizeGroupPageName(group.Key)+".md")
		if err := writeFileIfMissing(pagePath, generateGroupMarkdown(m, group)); err != nil {
			os.RemoveAll(tmpDir)
			return "", err
		}
	}

	if len(m.Services) > 0 {
		servicesDir := filepath.Join(tmpDir, "services")
		if err := os.MkdirAll(servicesDir, 0o755); err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("creating generated services directory: %w", err)
		}
		if err := writeFileIfMissing(filepath.Join(servicesDir, "_index.md"), generateServicesIndexMarkdown(m)); err != nil {
			os.RemoveAll(tmpDir)
			return "", err
		}
	}

	return tmpDir, nil
}

func copyFileIfExists(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking file %s: %w", src, err)
	}
	if info.IsDir() {
		return fmt.Errorf("file path %s is a directory", src)
	}

	return copyFile(src, dst)
}

func copyFileIfMissing(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking destination file %s: %w", dst, err)
	}

	return copyFileIfExists(src, dst)
}

func copyDirIfExists(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking content directory %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("content path %s is not a directory", src)
	}

	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		targetPath := filepath.Join(dst, relPath)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		return copyFile(path, targetPath)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return nil
}

func writeFileIfMissing(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking generated page path %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating page parent directory for %s: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing generated page %s: %w", path, err)
	}

	return nil
}

func generateHomeMarkdown(m *manifest.Manifest) string {
	var body strings.Builder
	title := strings.TrimSpace(defaultString(m.Meta.Title, "Configuration Reference"))
	description := normalizeMarkdownDescription(m.Meta.Description)
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       title,
		"description": description,
		"weight":      1,
	}))
	if description != "" {
		body.WriteString(description)
		if !strings.HasSuffix(description, "\n") {
			body.WriteString("\n")
		}
	}
	return body.String()
}

func normalizeMarkdownDescription(description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}

	bulletList := regexp.MustCompile(`\s+([*-])\s+`)
	description = bulletList.ReplaceAllString(description, "\n$1 ")

	orderedList := regexp.MustCompile(`\s+(\d+\.)\s+`)
	description = orderedList.ReplaceAllString(description, "\n$1 ")

	listToParagraph := regexp.MustCompile(`(\n(?:[*-]|\d+\.) [^\n]+)\n([A-Z])`)
	description = listToParagraph.ReplaceAllString(description, "$1\n\n$2")

	return description
}

func generateGroupsIndexMarkdown(m *manifest.Manifest) string {
	var body strings.Builder
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       "Groups",
		"description": "Auto-generated configuration group reference from env.yaml.",
		"weight":      10,
		"menu": map[string]interface{}{
			"main": map[string]interface{}{
				"name":   "Groups",
				"weight": 10,
			},
		},
	}))
	body.WriteString("This section is generated from env.yaml during `envy build`.\n\n")
	body.WriteString(renderCardsOpen(3))
	for _, group := range m.OrderedGroups() {
		description := strings.TrimSpace(defaultString(group.Description, "Group configuration"))
		body.WriteString(renderCard(group.Key, "/groups/"+sanitizeGroupPageName(group.Key)+"/", groupIcon(group.Key), description))
	}
	body.WriteString(renderCardsClose())
	return body.String()
}

func generateServicesIndexMarkdown(m *manifest.Manifest) string {
	var body strings.Builder
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       "Services",
		"description": "Auto-generated service reference from env.yaml.",
		"weight":      5,
		"menu": map[string]interface{}{
			"main": map[string]interface{}{
				"name":   "Services",
				"weight": 5,
			},
		},
	}))
	body.WriteString("This section is generated from env.yaml during `envy build`.\n\n")
	body.WriteString(renderCardsOpen(3))
	for _, service := range m.Services {
		description := strings.TrimSpace(defaultString(service.Description, service.Name+" service"))
		body.WriteString(renderCard(service.Name, "#"+service.Name, "serverless", description))
	}
	body.WriteString(renderCardsClose())
	return body.String()
}

func generateGroupMarkdown(m *manifest.Manifest, group manifest.Group) string {
	var body strings.Builder
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       group.Key,
		"description": strings.TrimSpace(group.Description),
		"weight":      groupWeight(m, group.Key),
	}))
	if strings.TrimSpace(group.Description) != "" {
		body.WriteString(strings.TrimSpace(group.Description) + "\n\n")
	}

	services := servicesForGroup(m, group.Key)
	if len(services) > 0 {
		body.WriteString("## Services\n\n")
		for _, service := range services {
			body.WriteString(fmt.Sprintf("- %s\n", service))
		}
		body.WriteString("\n")
	}

	if strings.TrimSpace(group.Link) != "" {
		body.WriteString(fmt.Sprintf("## Documentation\n\n![Documentation](/images/readme.svg) [%s](%s)\n\n", group.Link, group.Link))
	}

	body.WriteString("## Variables\n\n")
	if len(group.Vars) == 0 {
		body.WriteString("This group does not define variables.\n")
		return body.String()
	}
	for _, variable := range group.Vars {
		body.WriteString(fmt.Sprintf("<div id=\"%s\"></div>\n\n", variableHeadingAnchor(variable.Key)))
		body.WriteString(renderCardsOpen(1))
		tag, tagColor := variableCardTag(variable)
		body.WriteString(renderCardWithTag(variable.Key, "#"+variableHeadingAnchor(variable.Key), "env", variableCardSubtitle(variable), tag, tagColor))
		body.WriteString(renderCardsClose())
		body.WriteString("\n")
	}

	return body.String()
}

func frontMatterMarkdown(values map[string]interface{}) string {
	content, err := yaml.Marshal(values)
	if err != nil {
		return "---\n---\n\n"
	}
	return fmt.Sprintf("---\n%s---\n\n", string(content))
}

func sanitizeGroupPageName(groupKey string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	return replacer.Replace(groupKey)
}

func groupWeight(m *manifest.Manifest, groupKey string) int {
	for index, group := range m.OrderedGroups() {
		if group.Key == groupKey {
			return index + 1
		}
	}
	return 999
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func renderCardsOpen(columns int) string {
	return fmt.Sprintf("{{< cards cols=\"%d\" >}}\n", columns)
}

func renderCardsClose() string {
	return "{{< /cards >}}\n"
}

func renderCard(title, link, icon, description string) string {
	return renderCardWithTag(title, link, icon, description, "", "")
}

func renderCardWithTag(title, link, icon, description, tag, tagColor string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card link=\"%s\" title=\"%s\" icon=\"%s\"",
		escapeShortcodeValue(link),
		escapeShortcodeValue(title),
		escapeShortcodeValue(icon),
	))
	if strings.TrimSpace(description) != "" {
		sb.WriteString(fmt.Sprintf(" subtitle=`%s`", escapeShortcodeRawValue(description)))
	}
	if tag != "" {
		sb.WriteString(fmt.Sprintf(" tag=\"%s\"", escapeShortcodeValue(tag)))
		if tagColor != "" {
			sb.WriteString(fmt.Sprintf(" tagColor=\"%s\"", escapeShortcodeValue(tagColor)))
		}
	}
	sb.WriteString(" >}}\n")
	return sb.String()
}

func escapeShortcodeValue(value string) string {
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func escapeShortcodeRawValue(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}

func groupIcon(_ string) string {
	return "folder"
}

func variableCardTag(variable manifest.Var) (string, string) {
	if variable.IsSecret() {
		return "secret", "yellow"
	}
	return "", ""
}

func variableCardSubtitle(variable manifest.Var) string {
	parts := make([]string, 0, 5)
	if strings.TrimSpace(variable.Description) != "" {
		parts = append(parts, strings.TrimSpace(variable.Description))
	}

	meta := make([]string, 0, 2)
	if variable.IsRequired() {
		meta = append(meta, "Required")
	}
	if len(meta) > 0 {
		parts = append(parts, strings.Join(meta, " · "))
	}

	defaultValue := strings.TrimSpace(variable.DefaultString())
	if defaultValue != "" {
		if variable.IsSecret() {
			parts = append(parts, "Default hidden")
		} else {
			indentedDefault := "    " + strings.ReplaceAll(defaultValue, "\n", "\n    ")
			parts = append(parts, indentedDefault)
		}
	}

	if strings.TrimSpace(variable.Example) != "" {
		parts = append(parts, "Example: `"+strings.TrimSpace(variable.Example)+"`")
	}
	if len(parts) == 0 {
		return "Variable definition"
	}
	return strings.Join(parts, "\n\n")
}

func variableHeadingAnchor(key string) string {
	trimmed := strings.TrimSpace(strings.ToLower(key))
	trimmed = strings.ReplaceAll(trimmed, " ", "-")
	return trimmed
}

func servicesForGroup(m *manifest.Manifest, groupKey string) []string {
	services := make([]string, 0)
	for _, service := range m.Services {
		for _, serviceGroup := range service.Groups {
			if serviceGroup == groupKey {
				services = append(services, service.Name)
				break
			}
		}
	}
	return services
}
