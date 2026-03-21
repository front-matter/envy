package cmd

import (
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/front-matter/envy/compose"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var buildCmd = &cobra.Command{
	Use:                "build [hugo flags]",
	Short:              "Generate documentation site",
	Long:               "Generate documentation site for compose.yaml file using Hugo.",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHugoCommand("build", args)
	},
}

var serverCmd = &cobra.Command{
	Use:                "server [hugo flags]",
	Short:              "Run local documentation site",
	Long:               "Run local documentation site generated from compose.yaml",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHugoCommand("server", args)
	},
}

var deployCmd = &cobra.Command{
	Use:                "deploy [hugo flags]",
	Short:              "Deploy documentation site",
	Long:               "Deploy documentation site generated from compose.yaml",
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
			return fmt.Errorf("envy %s uses compose.yaml as Hugo config source; remove --config/-c", subcommand)
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
		return "", fmt.Errorf("envy build requires compose.yaml: %w\n\nSuggested fields in compose.yaml for Hugo:\n\nx-envy:\n  title: My Hugo Site\n  docs: https://example.org/\n  languageCode: en-US\n  ignoreLogs:\n    - warning-goldmark-raw-html\n\nsets:\n  hugo:\n    description: Hugo site configuration\n    vars:\n      HUGO_DEFAULT_CONTENT_LANGUAGE:\n        default: \"en\"\n      HUGO_TITLE:\n        default: \"My Hugo Site\"", err)
	}

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("envy build requires compose.yaml at %s\n\nSuggested fields in compose.yaml for Hugo:\n\nx-envy:\n  title: My Hugo Site\n  docs: https://example.org/\n  languageCode: en-US\n  ignoreLogs:\n    - warning-goldmark-raw-html\n\nsets:\n  hugo:\n    description: Hugo site configuration\n    vars:\n      HUGO_DEFAULT_CONTENT_LANGUAGE:\n        default: \"en\"\n      HUGO_TITLE:\n        default: \"My Hugo Site\"", path)
		}
		return "", fmt.Errorf("checking compose.yaml at %s: %w", path, err)
	}

	return path, nil
}

func prepareBuildAssets(path string) (string, error) {
	m, err := compose.Load(path)
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

func writeTempHugoConfigFromManifest(m *compose.Project, siteDir string) error {
	lookup := make(map[string]compose.Var)
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

func prepareBuildContentDir(siteRoot string, m *compose.Project) (string, error) {
	tmpDir, err := os.MkdirTemp("", "envy-hugo-content-*")
	if err != nil {
		return "", fmt.Errorf("creating temporary Hugo content directory: %w", err)
	}

	sourceContentDir := filepath.Join(siteRoot, "content")
	if err := copyDirIfExists(sourceContentDir, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	readmeHome := filepath.Join(siteRoot, "README.md")
	if err := copyFileIfMissing(readmeHome, filepath.Join(tmpDir, "_index.md")); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	setsDir := filepath.Join(tmpDir, "sets")
	if err := os.MkdirAll(setsDir, 0o755); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("creating generated sets directory: %w", err)
	}

	if err := writeFileIfMissing(filepath.Join(tmpDir, "_index.md"), generateHomeMarkdown(m)); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	if err := writeFileIfMissing(filepath.Join(setsDir, "_index.md"), generateSetsIndexMarkdown(m)); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	for _, set := range m.OrderedSets() {
		pagePath := filepath.Join(setsDir, sanitizeSetPageName(set.Key)+".md")
		if err := writeFileIfMissing(pagePath, generateSetMarkdown(m, set)); err != nil {
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

func generateHomeMarkdown(m *compose.Project) string {
	var body strings.Builder
	title := strings.TrimSpace(defaultString(m.Meta.Title, "Configuration Reference"))
	description := strings.TrimSpace(m.Meta.Description)
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

func generateSetsIndexMarkdown(m *compose.Project) string {
	var body strings.Builder
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       "Sets",
		"description": "Auto-generated configuration set reference from compose.yaml.",
		"weight":      10,
		"sidebar": map[string]interface{}{
			"hide": true,
		},
		"menu": map[string]interface{}{
			"main": map[string]interface{}{
				"name":   "Sets",
				"weight": 10,
			},
		},
	}))
	body.WriteString(renderCardsOpen(2))
	for _, set := range m.OrderedSets() {
		services := servicesForSet(m, set.Key)
		body.WriteString(renderSetOverviewCard(set, services))
	}
	body.WriteString(renderCardsClose())
	return body.String()
}

func generateServicesIndexMarkdown(m *compose.Project) string {
	var body strings.Builder
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       "Services",
		"description": "Auto-generated service reference from compose.yaml.",
		"weight":      5,
		"sidebar": map[string]interface{}{
			"hide": true,
		},
		"menu": map[string]interface{}{
			"main": map[string]interface{}{
				"name":   "Services",
				"weight": 5,
			},
		},
	}))
	body.WriteString(renderCardsOpen(2))
	for _, service := range m.Services {
		body.WriteString(renderServiceCard(m, service))
	}
	body.WriteString(renderCardsClose())
	return body.String()
}

func generateSetMarkdown(m *compose.Project, set compose.Set) string {
	var body strings.Builder
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       set.Key,
		"description": strings.TrimSpace(set.Description),
		"weight":      setWeight(m, set.Key),
		"hideTitle":   true,
		"toc":         false,
	}))

	// Add set card at the top
	services := servicesForSet(m, set.Key)
	body.WriteString(renderCardsOpen(1))
	body.WriteString(renderSetCard(set, services))
	body.WriteString(renderCardsClose())
	body.WriteString("\n")

	if len(set.Vars) == 0 {
		body.WriteString("This set does not define variables.\n")
		return body.String()
	}
	for _, variable := range set.Vars {
		body.WriteString(fmt.Sprintf("<div id=\"%s\"></div>\n\n", variableHeadingAnchor(variable.Key)))
		body.WriteString(renderCardsOpen(1))
		tag, tagColor, tagBorder := variableCardTag(variable)
		varClass := variableCardClass(variable)
		body.WriteString(renderCardWithTag(variable.Key, "", "env", variableCardSubtitle(variable), variableCardHTML(variable), "hx:py-4 hx:px-4", tag, tagColor, tagBorder, varClass))
		body.WriteString(renderCardsClose())
		body.WriteString("\n")
	}

	return body.String()
}

func renderServiceCard(m *compose.Project, service compose.Service) string {
	var sb strings.Builder
	titleClass := ""
	if len(service.Sets) > 0 {
		titleClass = "hx:pr-32 md:hx:pr-40"
	}
	sb.WriteString(fmt.Sprintf("{{< card title=\"%s\" titleLink=\"#%s\" icon=\"%s\"",
		escapeShortcodeValue(service.Name),
		escapeShortcodeValue(service.Name),
		"serverless",
	))

	if description := strings.TrimSpace(service.Description); description != "" {
		sb.WriteString(fmt.Sprintf(" subtitle=`%s`", escapeShortcodeRawValue(description)))
	}

	if strings.TrimSpace(service.Image) != "" {
		imageValue := strings.TrimSpace(service.Image)
		imageLink := imageLink(imageValue)
		if imageLink != "" {
			sb.WriteString(fmt.Sprintf(" subtitle2=`**Image:** [%s](%s)`", escapeShortcodeRawValue(imageValue), escapeShortcodeRawValue(imageLink)))
		} else {
			sb.WriteString(fmt.Sprintf(" subtitle2=`**Image:** %s`", escapeShortcodeRawValue(imageValue)))
		}
	}

	if platform := strings.TrimSpace(service.Platform); platform != "" {
		sb.WriteString(fmt.Sprintf(" subtitle3=`**Platform:** %s`", escapeShortcodeRawValue(platform)))
	}

	if len(service.Command) > 0 {
		indentedCommand := "**Command:**\n\n    " + wrapCommandArgs(service.Command, 60, "    ")
		sb.WriteString(fmt.Sprintf(" subtitle4=`%s`", escapeShortcodeRawValue(indentedCommand)))
	}

	if len(service.Sets) > 0 {
		setTags := make([]string, len(service.Sets))
		setTagLinks := make([]string, len(service.Sets))
		for i, setKey := range service.Sets {
			setTags[i] = setKey
			setTagLinks[i] = "/sets/" + sanitizeSetPageName(setKey) + "/"
		}
		sb.WriteString(fmt.Sprintf(` tags="%s" tagLinks="%s" tagColor="blue" tagBorder="false"`,
			escapeShortcodeValue(strings.Join(setTags, ",")),
			escapeShortcodeValue(strings.Join(setTagLinks, ",")),
		))
	}

	if titleClass != "" {
		sb.WriteString(fmt.Sprintf(" titleClass=\"%s\"", escapeShortcodeValue(titleClass)))
	}

	sb.WriteString(" >}}\n")
	return sb.String()
}

// imageLink generates a public registry URL for the given image reference.
// Supports Docker Hub (no registry or docker.io) and ghcr.io.
func imageLink(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}

	// Remove tag/digest (e.g., postgres:17.4 -> postgres)
	imageName := strings.Split(image, ":")[0]
	imageName = strings.Split(imageName, "@")[0]

	parts := strings.Split(imageName, "/")
	registry := ""
	if len(parts) > 1 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		registry = parts[0]
		parts = parts[1:]
		imageName = strings.Join(parts, "/")
	}

	switch registry {
	case "", "docker.io":
		// Docker Hub
		if len(parts) == 1 {
			// Official image: postgres -> https://hub.docker.com/_/postgres
			return fmt.Sprintf("https://hub.docker.com/_%s", "/"+imageName)
		} else if len(parts) == 2 {
			// User image: username/imagename -> https://hub.docker.com/r/username/imagename
			return fmt.Sprintf("https://hub.docker.com/r/%s", imageName)
		}
	case "ghcr.io":
		return fmt.Sprintf("https://ghcr.io/%s", imageName)
	}
	return ""
}

// wrapCommandArgs joins args into a shell command string, wrapping at maxWidth
// characters using backslash line continuation with the given continuation indent.
func wrapCommandArgs(args []string, maxWidth int, indent string) string {
	if len(args) == 0 {
		return ""
	}
	joined := strings.Join(args, " ")
	if len(joined) <= maxWidth {
		return joined
	}
	// Build wrapped output: first arg starts the line, subsequent args are added
	// until the line would exceed maxWidth, then a continuation is inserted.
	var sb strings.Builder
	lineLen := 0
	for i, arg := range args {
		if i == 0 {
			sb.WriteString(arg)
			lineLen = len(arg)
		} else {
			if lineLen+1+len(arg) > maxWidth {
				sb.WriteString(" \\\n")
				sb.WriteString(indent)
				sb.WriteString(arg)
				lineLen = len(indent) + len(arg)
			} else {
				sb.WriteString(" ")
				sb.WriteString(arg)
				lineLen += 1 + len(arg)
			}
		}
	}
	return sb.String()
}

func frontMatterMarkdown(values map[string]interface{}) string {
	content, err := yaml.Marshal(values)
	if err != nil {
		return "---\n---\n\n"
	}
	return fmt.Sprintf("---\n%s---\n\n", string(content))
}

func sanitizeSetPageName(setKey string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	return replacer.Replace(setKey)
}

func setWeight(m *compose.Project, setKey string) int {
	for index, set := range m.OrderedSets() {
		if set.Key == setKey {
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
	return renderCardWithTag(title, link, icon, description, "", "", "", "", "", "")
}

func renderCardWithTag(title, link, icon, description, htmlContent, titlePadding, tag, tagColor, tagBorder, class string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card link=\"%s\" title=\"%s\" icon=\"%s\"",
		escapeShortcodeValue(link),
		escapeShortcodeValue(title),
		escapeShortcodeValue(icon),
	))
	if strings.TrimSpace(description) != "" {
		sb.WriteString(fmt.Sprintf(" subtitle=`%s`", escapeShortcodeRawValue(description)))
	}
	if strings.TrimSpace(htmlContent) != "" {
		sb.WriteString(fmt.Sprintf(" html=`%s`", escapeShortcodeRawValue(htmlContent)))
	}
	if strings.TrimSpace(titlePadding) != "" {
		sb.WriteString(fmt.Sprintf(" titlePadding=\"%s\"", escapeShortcodeValue(titlePadding)))
	}
	if tag != "" {
		sb.WriteString(fmt.Sprintf(" tag=\"%s\"", escapeShortcodeValue(tag)))
		if tagColor != "" {
			sb.WriteString(fmt.Sprintf(" tagColor=\"%s\"", escapeShortcodeValue(tagColor)))
		}
		if tagBorder != "" {
			sb.WriteString(fmt.Sprintf(" tagBorder=\"%s\"", escapeShortcodeValue(tagBorder)))
		}
	}
	if class != "" {
		sb.WriteString(fmt.Sprintf(" class=\"%s\"", escapeShortcodeValue(class)))
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

func renderSetCard(set compose.Set, services []string) string {
	var sb strings.Builder
	titleClass := "hx:text-4xl md:hx:text-5xl hx:tracking-tight"
	if len(services) > 0 {
		titleClass += " hx:pr-40 md:hx:pr-56"
	}
	sb.WriteString(fmt.Sprintf("{{< card title=\"%s\" iconImage=\"%s\" iconImageClass=\"%s\" titleClass=\"%s\"",
		escapeShortcodeValue(set.Key),
		escapeShortcodeValue("/images/properties.svg"),
		escapeShortcodeValue("hx:h-8 hx:w-8 md:h-10 md:w-10 hx:shrink-0"),
		escapeShortcodeValue(titleClass),
	))

	// Add description as subtitle
	if strings.TrimSpace(set.Description) != "" {
		sb.WriteString(fmt.Sprintf(" subtitle=`%s`", escapeShortcodeRawValue(strings.TrimSpace(set.Description))))
	}

	// Add documentation link as subtitle2
	if strings.TrimSpace(set.Link) != "" {
		link := strings.TrimSpace(set.Link)
		sb.WriteString(fmt.Sprintf(" subtitle2=`[%s](%s)`", escapeShortcodeRawValue(link), escapeShortcodeRawValue(link)))
	}

	// Add services tags in the card tag area
	if len(services) > 0 {
		serviceTags := make([]string, len(services))
		serviceTagLinks := make([]string, len(services))
		for i, service := range services {
			serviceTags[i] = service
			serviceTagLinks[i] = "/services/#" + service
		}
		sb.WriteString(fmt.Sprintf(` tags="%s" tagLinks="%s" tagColor="red" tagBorder="false"`,
			escapeShortcodeValue(strings.Join(serviceTags, ",")),
			escapeShortcodeValue(strings.Join(serviceTagLinks, ",")),
		))
	}

	sb.WriteString(" >}}\n")
	return sb.String()
}

func renderSetOverviewCard(set compose.Set, services []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card title=\"%s\" titleLink=\"%s\" iconImage=\"%s\" iconImageClass=\"%s\"",
		escapeShortcodeValue(set.Key),
		escapeShortcodeValue("/sets/"+sanitizeSetPageName(set.Key)+"/"),
		escapeShortcodeValue("/images/properties.svg"),
		escapeShortcodeValue("hx:h-8 hx:w-8 md:h-10 md:w-10 hx:shrink-0"),
	))

	description := strings.TrimSpace(defaultString(set.Description, "Set configuration"))
	if description != "" {
		sb.WriteString(fmt.Sprintf(" subtitle=`%s`", escapeShortcodeRawValue(description)))
	}

	if strings.TrimSpace(set.Link) != "" {
		link := strings.TrimSpace(set.Link)
		sb.WriteString(fmt.Sprintf(" subtitle2=`[%s](%s)`", escapeShortcodeRawValue(link), escapeShortcodeRawValue(link)))
	}

	if len(services) > 0 {
		serviceTags := make([]string, len(services))
		serviceTagLinks := make([]string, len(services))
		for i, service := range services {
			serviceTags[i] = service
			serviceTagLinks[i] = "/services/#" + service
		}
		sb.WriteString(fmt.Sprintf(` tags="%s" tagLinks="%s" tagColor="red" tagBorder="false"`,
			escapeShortcodeValue(strings.Join(serviceTags, ",")),
			escapeShortcodeValue(strings.Join(serviceTagLinks, ",")),
		))
	}

	sb.WriteString(" >}}\n")
	return sb.String()
}

func renderLinkedHextraBadgeHTML(label, href, color string, border bool) string {
	badgeClass := hextraBadgeColorClass(color)
	borderClass := ""
	if border {
		borderClass = "hx:border "
	}

	return fmt.Sprintf(
		"<a href=\"%s\" title=\"%s\" class=\"not-prose hx:inline-flex hx:align-middle hx:no-underline hover:hx:no-underline\"><div class=\"hextra-badge\"><div class=\"hx:inline-flex hx:gap-1 hx:items-center hx:rounded-full hx:px-2.5 hx:leading-6 hx:text-[.65rem] %s%s\">%s</div></div></a>",
		html.EscapeString(href),
		html.EscapeString(label),
		borderClass,
		badgeClass,
		html.EscapeString(label),
	)
}

func renderHextraBadgeHTML(label, color string) string {
	badgeClass := hextraBadgeColorClass(color)

	return fmt.Sprintf(
		"<div class=\"hextra-badge\"><div class=\"hx:inline-flex hx:gap-1 hx:items-center hx:rounded-full hx:px-2.5 hx:leading-6 hx:text-[.65rem] hx:border %s\">%s</div></div>",
		badgeClass,
		html.EscapeString(label),
	)
}

func hextraBadgeColorClass(color string) string {
	badgeClass := ""
	switch color {
	case "purple":
		badgeClass = "hx:border-purple-200 hx:bg-purple-100 hx:text-purple-900 hx:dark:border-purple-200/30 hx:dark:bg-purple-900/30 hx:dark:text-purple-200"
	case "indigo":
		badgeClass = "hx:border-indigo-200 hx:bg-indigo-100 hx:text-indigo-900 hx:dark:border-indigo-200/30 hx:dark:bg-indigo-900/30 hx:dark:text-indigo-200"
	case "blue":
		badgeClass = "hx:border-blue-200 hx:bg-blue-100 hx:text-blue-900 hx:dark:border-blue-200/30 hx:dark:bg-blue-900/30 hx:dark:text-blue-200"
	case "green":
		badgeClass = "hx:border-green-200 hx:bg-green-100 hx:text-green-900 hx:dark:border-green-200/30 hx:dark:bg-green-900/30 hx:dark:text-green-200"
	case "yellow":
		badgeClass = "hx:border-yellow-100 hx:bg-yellow-50 hx:text-yellow-900 hx:dark:border-yellow-200/30 hx:dark:bg-yellow-700/30 hx:dark:text-yellow-200"
	case "orange":
		badgeClass = "hx:border-orange-100 hx:bg-orange-50 hx:text-orange-800 hx:dark:border-orange-400/30 hx:dark:bg-orange-400/20 hx:dark:text-orange-300"
	case "amber":
		badgeClass = "hx:border-amber-200 hx:bg-amber-100 hx:text-amber-900 hx:dark:border-amber-200/30 hx:dark:bg-amber-900/30 hx:dark:text-amber-200"
	case "red":
		badgeClass = "hx:border-red-200 hx:bg-red-100 hx:text-red-900 hx:dark:border-red-200/30 hx:dark:bg-red-900/30 hx:dark:text-red-200"
	default:
		badgeClass = "hx:text-gray-600 hx:bg-gray-100 hx:dark:bg-neutral-800 hx:dark:text-neutral-200 hx:border-gray-200 hx:dark:border-neutral-700"
	}

	return badgeClass
}

func setIcon(_ string) string {
	return "folder"
}

func variableCardTag(variable compose.Var) (string, string, string) {
	if variable.IsSecret() {
		return "secret", "orange", "false"
	}
	if variable.IsReadonly() {
		return "readonly", "yellow", "false"
	}
	return "", "", ""
}

func variableCardClass(variable compose.Var) string {
	var classes []string
	if variable.IsReadonly() {
		classes = append(classes, "read-only")
	}
	if variable.IsRequired() {
		classes = append(classes, "[&:user-invalid]:hx:border-red-500 [&:user-invalid]:hx:bg-red-50 [&:user-invalid]:hx:dark:bg-red-900/20")
	}
	return strings.Join(classes, " ")
}

func variableCardSubtitle(variable compose.Var) string {
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
		}
	}

	if strings.TrimSpace(variable.Example) != "" {
		parts = append(parts, "Example: `"+strings.TrimSpace(variable.Example)+"`")
	}
	if len(parts) == 0 {
		if defaultValue != "" || variable.IsSecret() {
			return ""
		}
	}
	return strings.Join(parts, "\n\n")
}

func variableCardHTML(variable compose.Var) string {
	defaultValue := strings.TrimSpace(variable.DefaultString())
	if defaultValue == "" || variable.IsSecret() {
		return ""
	}

	editable := "true"
	preClass := "hx:mt-0 hx:mb-4 hx:overflow-x-auto hx:rounded-md hx:border hx:px-3 hx:py-2 hx:text-sm hx:transition-colors"
	codeClass := "hx:block hx:whitespace-pre hx:font-mono hx:outline-none"
	if variable.IsReadonly() {
		editable = "false"
		preClass += " hx:border-yellow-200 hx:bg-yellow-50/80 hx:text-yellow-950 hx:dark:border-yellow-200/30 hx:dark:bg-yellow-900/20 hx:dark:text-yellow-100"
		codeClass += " hx:cursor-default hx:select-text"
	} else {
		preClass += " hx:border-blue-200 hx:bg-blue-50/70 hx:text-blue-950 hx:shadow-sm hx:dark:border-blue-200/30 hx:dark:bg-blue-900/20 hx:dark:text-blue-100"
		codeClass += " hx:cursor-text focus:hx:bg-white/60 hx:dark:focus:hx:bg-blue-950/20"
	}

	return fmt.Sprintf(`<div class="not-prose hx:px-4 hx:pb-4"><pre class="%s" data-editable="%s"><code class="%s" contenteditable="%s" spellcheck="false">%s</code></pre></div>`, preClass, editable, codeClass, editable, html.EscapeString(defaultValue))
}

func variableHeadingAnchor(key string) string {
	trimmed := strings.TrimSpace(strings.ToLower(key))
	trimmed = strings.ReplaceAll(trimmed, " ", "-")
	return trimmed
}

func servicesForSet(m *compose.Project, setKey string) []string {
	services := make([]string, 0)
	for _, service := range m.Services {
		for _, serviceSet := range service.Sets {
			if serviceSet == setKey {
				services = append(services, service.Name)
				break
			}
		}
	}
	return services
}
