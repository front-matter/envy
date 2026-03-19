package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/front-matter/envy/manifest"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const hextraModulePath = "github.com/imfing/hextra"

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

	if err := ensureHextraModule(hugoPath); err != nil {
		return err
	}

	buildConfigPath := ""
	buildContentDir := ""
	if subcommand == "build" {
		if hasConfigFlag(args) {
			return fmt.Errorf("envy build uses env.yaml as Hugo config source; remove --config/-c")
		}

		manifestFilePath, err := resolveBuildManifestPath()
		if err != nil {
			return err
		}

		buildConfigPath, buildContentDir, err = prepareBuildAssets(manifestFilePath)
		if err != nil {
			return err
		}
		defer os.Remove(buildConfigPath)
		defer os.RemoveAll(buildContentDir)
	}

	allArgs := make([]string, 0, len(args)+1)
	allArgs = append(allArgs, subcommand)
	if subcommand == "build" {
		allArgs = append(allArgs, "--config", buildConfigPath)
	}
	allArgs = append(allArgs, args...)

	command := exec.Command(hugoPath, allArgs...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("hugo %s exited with code %d", subcommand, exitErr.ExitCode())
		}
		return fmt.Errorf("running hugo %s: %w", subcommand, err)
	}

	return nil
}

func ensureHextraModule(hugoPath string) error {
	// Keep Hextra available in the active Hugo site before running forwarded commands.
	command := exec.Command(hugoPath, "mod", "get", hextraModulePath)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	if err := command.Run(); err != nil {
		return fmt.Errorf("adding hugo module %s: %w", hextraModulePath, err)
	}

	return nil
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

type hugoGeneratedConfig struct {
	BaseURL                string            `yaml:"baseURL,omitempty"`
	LanguageCode           string            `yaml:"languageCode,omitempty"`
	DefaultContentLanguage string            `yaml:"defaultContentLanguage,omitempty"`
	Title                  string            `yaml:"title,omitempty"`
	ContentDir             string            `yaml:"contentDir,omitempty"`
	Module                 hugoGeneratedMods `yaml:"module"`
}

type hugoGeneratedMods struct {
	Imports []hugoGeneratedImport `yaml:"imports,omitempty"`
}

type hugoGeneratedImport struct {
	Path string `yaml:"path"`
}

func prepareBuildAssets(path string) (string, string, error) {
	m, err := manifest.Load(path)
	if err != nil {
		return "", "", err
	}

	contentDir, err := prepareBuildContentDir(".", m)
	if err != nil {
		return "", "", err
	}

	configPath, err := writeTempHugoConfigFromManifest(m, contentDir)
	if err != nil {
		os.RemoveAll(contentDir)
		return "", "", err
	}

	return configPath, contentDir, nil
}

func writeTempHugoConfigFromManifest(m *manifest.Manifest, contentDir string) (string, error) {
	lookup := make(map[string]manifest.Var)
	for _, v := range m.AllVars() {
		lookup[v.Key] = v
	}

	config := hugoGeneratedConfig{
		ContentDir: contentDir,
		Module: hugoGeneratedMods{
			Imports: []hugoGeneratedImport{{Path: hextraModulePath}},
		},
	}

	if m.Meta.Docs != "" {
		config.BaseURL = m.Meta.Docs
	}
	config.LanguageCode = m.Meta.LanguageCodeLabel()
	if v, ok := lookup["HUGO_LANGUAGE_CODE"]; ok && strings.TrimSpace(v.DefaultString()) != "" {
		config.LanguageCode = v.DefaultString()
	}
	if v, ok := lookup["HUGO_DEFAULT_CONTENT_LANGUAGE"]; ok && strings.TrimSpace(v.DefaultString()) != "" {
		config.DefaultContentLanguage = v.DefaultString()
	}
	if v, ok := lookup["HUGO_TITLE"]; ok && strings.TrimSpace(v.DefaultString()) != "" {
		config.Title = v.DefaultString()
	} else if strings.TrimSpace(m.Meta.Title) != "" {
		config.Title = m.Meta.Title
	}

	content, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("rendering temporary Hugo config: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "envy-hugo-*.yaml")
	if err != nil {
		return "", fmt.Errorf("creating temporary Hugo config: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(content); err != nil {
		return "", fmt.Errorf("writing temporary Hugo config: %w", err)
	}

	return tmpFile.Name(), nil
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

	return tmpDir, nil
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
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       strings.TrimSpace(defaultString(m.Meta.Title, "Configuration Reference")),
		"description": strings.TrimSpace(defaultString(m.Meta.Description, "Auto-generated documentation from env.yaml.")),
		"weight":      1,
	}))
	body.WriteString(fmt.Sprintf("# %s\n\n", strings.TrimSpace(defaultString(m.Meta.Title, "Configuration Reference"))))
	if strings.TrimSpace(m.Meta.Description) != "" {
		body.WriteString(strings.TrimSpace(m.Meta.Description) + "\n\n")
	} else {
		body.WriteString("This site is generated from env.yaml during `envy build`.\n\n")
	}
	body.WriteString("## Overview\n\n")
	body.WriteString(fmt.Sprintf("- Groups: %d\n", len(m.Groups)))
	body.WriteString(fmt.Sprintf("- Variables: %d\n", len(m.AllVars())))
	if strings.TrimSpace(m.Meta.Docs) != "" {
		body.WriteString(fmt.Sprintf("- Docs: [%s](%s)\n", m.Meta.Docs, m.Meta.Docs))
	}
	body.WriteString("\n## Navigation\n\n")
	body.WriteString(renderCardsOpen(2))
	body.WriteString(renderCard("Browse all groups", "/groups/", "folder-open", "Open the generated reference for every env.yaml group."))
	if strings.TrimSpace(m.Meta.Docs) != "" {
		body.WriteString(renderCard("Project docs", m.Meta.Docs, "book-open", "Jump to the project or product documentation linked from meta.docs."))
	}
	body.WriteString(renderCardsClose())
	body.WriteString("\n### Group cards\n\n")
	body.WriteString(renderCardsOpen(3))
	for _, group := range m.OrderedGroups() {
		description := strings.TrimSpace(defaultString(group.Description, "Group configuration"))
		body.WriteString(renderCard(group.Key, "/groups/"+sanitizeGroupPageName(group.Key)+"/", groupIcon(group.Key), description))
	}
	body.WriteString(renderCardsClose())
	body.WriteString("\n")
	body.WriteString("- [Browse all groups](/groups/)\n")
	return body.String()
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
				"weight": 20,
			},
		},
	}))
	body.WriteString("# Groups\n\n")
	body.WriteString("This section is generated from env.yaml during `envy build`.\n\n")
	body.WriteString("- [Back to home](/)\n\n")
	body.WriteString(renderCardsOpen(3))
	for _, group := range m.OrderedGroups() {
		description := strings.TrimSpace(defaultString(group.Description, "Group configuration"))
		body.WriteString(renderCard(group.Key, "/groups/"+sanitizeGroupPageName(group.Key)+"/", groupIcon(group.Key), description))
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
	body.WriteString(fmt.Sprintf("# %s\n\n", group.Key))
	body.WriteString("- [Home](/)\n")
	body.WriteString("- [All groups](/groups/)\n\n")
	if strings.TrimSpace(group.Description) != "" {
		body.WriteString(strings.TrimSpace(group.Description) + "\n\n")
	}
	body.WriteString(renderCardsOpen(2))
	body.WriteString(renderCard("Back to groups", "/groups/", "folder-open", "Return to the complete group catalog."))
	if strings.TrimSpace(group.Link) != "" {
		body.WriteString(renderCard("External documentation", group.Link, "book-open", "Open the upstream docs linked in env.yaml."))
	}
	body.WriteString(renderCardsClose())
	body.WriteString("\n")

	services := servicesForGroup(m, group.Key)
	if len(services) > 0 {
		body.WriteString("## Services\n\n")
		for _, service := range services {
			body.WriteString(fmt.Sprintf("- %s\n", service))
		}
		body.WriteString("\n")
	}

	if strings.TrimSpace(group.Link) != "" {
		body.WriteString(fmt.Sprintf("## Documentation\n\n- [%s](%s)\n\n", group.Link, group.Link))
	}

	body.WriteString("## Variables\n\n")
	if len(group.Vars) == 0 {
		body.WriteString("This group does not define variables.\n")
		return body.String()
	}
	for _, variable := range group.Vars {
		body.WriteString(fmt.Sprintf("### %s\n\n", variable.Key))
		body.WriteString(renderCardsOpen(1))
		body.WriteString(renderCard(variable.Key, "#"+variableHeadingAnchor(variable.Key), "tag", variableCardSubtitle(variable)))
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
	if strings.TrimSpace(description) != "" {
		return fmt.Sprintf(
			"{{< card link=\"%s\" title=\"%s\" icon=\"%s\" subtitle=\"%s\" >}}\n",
			escapeShortcodeValue(link),
			escapeShortcodeValue(title),
			escapeShortcodeValue(icon),
			escapeShortcodeValue(description),
		)
	}

	return fmt.Sprintf(
		"{{< card link=\"%s\" title=\"%s\" icon=\"%s\" >}}\n",
		escapeShortcodeValue(link),
		escapeShortcodeValue(title),
		escapeShortcodeValue(icon),
	)
}

func escapeShortcodeValue(value string) string {
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func groupIcon(groupKey string) string {
	switch strings.ToLower(strings.TrimSpace(groupKey)) {
	case "authentication":
		return "shield-check"
	case "cache":
		return "bolt"
	case "db":
		return "circle-stack"
	case "doi":
		return "finger-print"
	case "mail":
		return "envelope"
	case "s3":
		return "cloud-arrow-up"
	case "search":
		return "magnifying-glass"
	case "web":
		return "globe-alt"
	default:
		return "cog-6-tooth"
	}
}

func variableCardSubtitle(variable manifest.Var) string {
	parts := make([]string, 0, 4)
	if strings.TrimSpace(variable.Description) != "" {
		parts = append(parts, strings.TrimSpace(variable.Description))
	}
	if variable.IsRequired() {
		parts = append(parts, "Required")
	}
	if variable.IsSecret() {
		parts = append(parts, "Secret")
	}
	defaultValue := strings.TrimSpace(variable.DefaultString())
	if defaultValue != "" {
		if variable.IsSecret() {
			parts = append(parts, "Default hidden")
		} else {
			parts = append(parts, "Default: `"+defaultValue+"`")
		}
	}
	if strings.TrimSpace(variable.Example) != "" {
		parts = append(parts, "Example: `"+strings.TrimSpace(variable.Example)+"`")
	}
	if len(parts) == 0 {
		return "Variable definition"
	}
	return strings.Join(parts, " · ")
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
