package cmd

import (
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/front-matter/envy/compose"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const hugoModuleVersion = "github.com/gohugoio/hugo@v0.156.0"

var (
	docsI18nOnce sync.Once
	docsI18n     map[string]map[string]string
	docsI18nErr  error
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
	watchEnabled := false
	if subcommand == "server" {
		var err error
		watchEnabled, args, err = parseWatchFlag(args)
		if err != nil {
			return err
		}
	} else if hasFlag(args, "--watch") {
		return fmt.Errorf("--watch is only supported with envy server")
	}

	if subcommand == "server" && watchEnabled {
		return runHugoServerWithWatch(args)
	}

	hugoArgs := make([]string, 0, len(args)+3)
	hugoArgs = append(hugoArgs, subcommand)
	refreshPersistentContent := subcommand == "build" && hasFlag(args, "--cleanDestinationDir")

	buildSiteDir := ""
	if usesGeneratedHugoSite(subcommand) {
		if hasConfigFlag(args) {
			return fmt.Errorf("envy %s uses compose.yaml as Hugo config source; remove --config/-c", subcommand)
		}

		manifestFilePath, err := resolveBuildManifestPath()
		if err != nil {
			return err
		}

		if subcommand == "build" {
			if err := validateComposeFile(manifestFilePath); err != nil {
				return fmt.Errorf("envy build aborted: validation failed: %w", err)
			}
		}

		buildSiteDir, err = prepareBuildAssets(manifestFilePath, refreshPersistentContent)
		if err != nil {
			return err
		}
		defer os.RemoveAll(buildSiteDir)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determining working directory: %w", err)
	}

	if subcommand == "build" {
		hugoArgs = append(hugoArgs, "--destination", filepath.Join(cwd, "public"))
		if !hasFlag(args, "--cleanDestinationDir") {
			hugoArgs = append(hugoArgs, "--cleanDestinationDir")
		}
	}
	hugoArgs = append(hugoArgs, args...)

	command, commandName := buildHugoExecCommand(hugoArgs)
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
			return fmt.Errorf("%s %s exited with code %d", commandName, subcommand, exitErr.ExitCode())
		}
		return fmt.Errorf("running %s %s: %w", commandName, subcommand, err)
	}

	return nil
}

func runHugoServerWithWatch(args []string) error {
	if hasConfigFlag(args) {
		return fmt.Errorf("envy server uses compose.yaml as Hugo config source; remove --config/-c")
	}

	manifestFilePath, err := resolveBuildManifestPath()
	if err != nil {
		return err
	}

	hugoArgs := make([]string, 0, len(args)+1)
	hugoArgs = append(hugoArgs, "server")
	hugoArgs = append(hugoArgs, args...)

	buildSiteDir, err := prepareBuildAssets(manifestFilePath, false)
	if err != nil {
		return err
	}
	defer os.RemoveAll(buildSiteDir)

	command, _ := buildHugoExecCommand(hugoArgs)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Dir = buildSiteDir
	command.Env = append(os.Environ(),
		"GOFLAGS=-mod=vendor",
		"GONOSUMDB=*",
		"GONOSUMCHECK=*",
	)

	if startErr := command.Start(); startErr != nil {
		return fmt.Errorf("starting hugo server: %w", startErr)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- command.Wait()
	}()

	fileState, err := composeManifestState(manifestFilePath)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-sigCh:
			if command.Process != nil {
				_ = command.Process.Signal(os.Interrupt)
				select {
				case <-waitCh:
					return nil
				case <-time.After(2 * time.Second):
					if err := command.Process.Kill(); err != nil {
						return err
					}
					<-waitCh
				}
			}
			return nil
		case waitErr := <-waitCh:
			if waitErr != nil {
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					return fmt.Errorf("hugo server exited with code %d", exitErr.ExitCode())
				}
				return fmt.Errorf("running hugo server: %w", waitErr)
			}
			return nil
		case <-ticker.C:
			changed, nextState, stateErr := composeManifestChanged(manifestFilePath, fileState)
			if stateErr != nil {
				return stateErr
			}
			if !changed {
				continue
			}

			fmt.Fprintln(os.Stderr, "compose.yml changed, regenerating documentation content...")
			if err := syncBuildAssets(manifestFilePath, buildSiteDir, false); err != nil {
				fmt.Fprintf(os.Stderr, "failed to regenerate site assets: %v\n", err)
			}
			fileState = nextState
		}
	}
}

func parseWatchFlag(args []string) (bool, []string, error) {
	watchEnabled := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--watch" {
			watchEnabled = true
			continue
		}
		if strings.HasPrefix(arg, "--watch=") {
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--watch="))
			if value == "" {
				watchEnabled = true
				continue
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return false, nil, fmt.Errorf("invalid --watch value %q; use true/false", value)
			}
			watchEnabled = parsed
			continue
		}
		filtered = append(filtered, arg)
	}

	return watchEnabled, filtered, nil
}

type manifestFileState struct {
	modTime time.Time
	size    int64
}

func composeManifestState(path string) (manifestFileState, error) {
	info, err := os.Stat(path)
	if err != nil {
		return manifestFileState{}, fmt.Errorf("checking compose.yaml at %s: %w", path, err)
	}
	return manifestFileState{modTime: info.ModTime(), size: info.Size()}, nil
}

func composeManifestChanged(path string, previous manifestFileState) (bool, manifestFileState, error) {
	current, err := composeManifestState(path)
	if err != nil {
		return false, manifestFileState{}, err
	}
	if !current.modTime.Equal(previous.modTime) || current.size != previous.size {
		return true, current, nil
	}
	return false, previous, nil
}

func buildHugoExecCommand(args []string) (*exec.Cmd, string) {
	hugoPath, err := exec.LookPath("hugo")
	if err == nil {
		return exec.Command(hugoPath, args...), "hugo"
	}

	goArgs := append([]string{"run", "-mod=mod", hugoModuleVersion}, args...)
	return exec.Command("go", goArgs...), "go run " + hugoModuleVersion
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

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
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

func prepareBuildAssets(path string, refreshPersistentContent bool) (string, error) {
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

	if err := syncBuildAssets(path, siteDir, refreshPersistentContent); err != nil {
		os.RemoveAll(siteDir)
		return "", err
	}

	return siteDir, nil
}

func syncBuildAssets(path, siteDir string, refreshPersistentContent bool) error {
	m, err := compose.Load(path)
	if err != nil {
		return err
	}

	manifestDir := filepath.Dir(path)

	contentDir, err := prepareBuildContentDirWithOptions(manifestDir, m, refreshPersistentContent)
	if err != nil {
		return err
	}

	contentDst := filepath.Join(siteDir, "content")
	if err := os.RemoveAll(contentDst); err != nil {
		return fmt.Errorf("clearing generated content directory: %w", err)
	}

	if err := copyDirIfExists(contentDir, contentDst); err != nil {
		return err
	}

	dataDst := filepath.Join(siteDir, "data")
	if err := copyDirIfExists(filepath.Join(manifestDir, "data"), dataDst); err != nil {
		return err
	}

	repoURL := detectRepositoryURL(manifestDir)

	if err := writeTempHugoConfigFromManifest(m, siteDir, repoURL); err != nil {
		return err
	}

	return nil
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

func writeTempHugoConfigFromManifest(m *compose.Project, siteDir string, repoURL string) error {
	lookup := buildVarLookup(m)
	defaultLanguage := strings.TrimSpace(hugoConfigValue(m.Meta.HugoDefaultLanguage, lookup, "HUGO_DEFAULT_CONTENT_LANGUAGE"))
	if defaultLanguage == "" {
		defaultLanguage = "en"
	}
	menuTitle := strings.TrimSpace(hugoConfigValue(m.Meta.HugoTitle, lookup, "HUGO_TITLE"))
	if menuTitle == "" {
		menuTitle = strings.TrimSpace(m.Meta.Title)
	}
	menuMain := localizedMenuMain(defaultLanguage, repoURL, menuTitle, false)

	config := map[string]interface{}{
		"module": map[string]interface{}{
			"imports": []map[string]string{{"path": "github.com/imfing/hextra"}},
		},
		"menu": map[string]interface{}{
			"main": menuMain,
		},
		"security": map[string]interface{}{
			"enableInlineShortcodes": true,
		},
	}

	if m.Meta.Docs != "" {
		config["baseURL"] = m.Meta.Docs
	}
	config["languageCode"] = m.Meta.LanguageCodeLabel()
	if value := hugoConfigValue(m.Meta.HugoLanguageCode, lookup, "HUGO_LANGUAGE_CODE"); value != "" {
		config["languageCode"] = value
	}
	if value := hugoConfigValue(m.Meta.HugoDefaultLanguage, lookup, "HUGO_DEFAULT_CONTENT_LANGUAGE"); value != "" {
		config["defaultContentLanguage"] = value
	}
	if value := hugoConfigValue(m.Meta.HugoDefaultInSubdir, lookup, "HUGO_DEFAULT_CONTENT_LANGUAGE_IN_SUBDIR"); value != "" {
		if parsed, ok := parseBoolString(value); ok {
			config["defaultContentLanguageInSubdir"] = parsed
		}
	}
	if value := hugoConfigValue(m.Meta.HugoLanguages, lookup, "HUGO_LANGUAGES"); value != "" {
		languages, err := parseHugoLanguages(value)
		if err != nil {
			return fmt.Errorf("parsing HUGO_LANGUAGES: %w", err)
		}
		includeLanguageSwitch := len(languages) > 1
		config["languages"] = localizedLanguagesConfig(languages, repoURL, menuTitle, includeLanguageSwitch)

		if includeLanguageSwitch {
			config["menu"] = map[string]interface{}{
				"main": localizedMenuMain(defaultLanguage, repoURL, menuTitle, true),
			}
		}
	}
	if value := hugoConfigValue(m.Meta.HugoTitle, lookup, "HUGO_TITLE"); value != "" {
		config["title"] = value
	} else if strings.TrimSpace(m.Meta.Title) != "" {
		config["title"] = m.Meta.Title
	}

	params := map[string]interface{}{
		"navbar": map[string]interface{}{
			"displayTitle": false,
			"logo": map[string]interface{}{
				"path": "images/logo.svg",
			},
		},
	}
	if description := strings.TrimSpace(m.Meta.HugoParamsDescription); description != "" {
		params["description"] = description
	}
	config["params"] = params

	if len(m.Meta.HugoIgnoreLogs) > 0 {
		config["ignoreLogs"] = m.Meta.HugoIgnoreLogs
	} else if len(m.Meta.IgnoreLogs) > 0 {
		config["ignoreLogs"] = m.Meta.IgnoreLogs
	}
	markupUnsafe := strings.TrimSpace(m.Meta.HugoMarkupGoldmarkUnsafe)
	if markupUnsafe == "" {
		markupUnsafe = strings.TrimSpace(m.Meta.MarkupGoldmarkUnsafe)
	}
	if markupUnsafe == "true" {
		config["markup"] = map[string]interface{}{
			"goldmark": map[string]interface{}{
				"renderer": map[string]interface{}{
					"unsafe": true,
				},
			},
		}
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

func parseBoolString(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func hugoConfigValue(metaValue string, lookup map[string]compose.Var, key string) string {
	if trimmed := strings.TrimSpace(metaValue); trimmed != "" {
		return trimmed
	}
	if v, ok := lookup[key]; ok {
		return strings.TrimSpace(v.DefaultString())
	}
	return ""
}

func parseHugoLanguages(raw string) (map[string]interface{}, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	var languages map[string]interface{}
	if err := yaml.Unmarshal([]byte(trimmed), &languages); err != nil {
		return nil, err
	}
	if len(languages) == 0 {
		return nil, fmt.Errorf("languages map is empty")
	}

	return languages, nil
}

func loadDocsI18n() (map[string]map[string]string, error) {
	docsI18nOnce.Do(func() {
		docsI18n = make(map[string]map[string]string)
		for _, language := range []string{"en", "de"} {
			path := "docs/i18n/" + language + ".yaml"
			data, err := fs.ReadFile(docsFS, path)
			if err != nil {
				docsI18nErr = fmt.Errorf("reading %s: %w", path, err)
				return
			}

			translations := make(map[string]string)
			if err := yaml.Unmarshal(data, &translations); err != nil {
				docsI18nErr = fmt.Errorf("parsing %s: %w", path, err)
				return
			}
			docsI18n[language] = translations
		}
	})

	return docsI18n, docsI18nErr
}

func localizedString(language, key, fallback string) string {
	translations, err := loadDocsI18n()
	if err != nil {
		return fallback
	}

	for _, candidate := range translationCandidates(language) {
		if languageTranslations, ok := translations[candidate]; ok {
			if value := strings.TrimSpace(languageTranslations[key]); value != "" {
				return value
			}
		}
	}

	return fallback
}

func translationCandidates(language string) []string {
	trimmed := strings.TrimSpace(strings.ToLower(language))
	if trimmed == "" {
		return []string{"en"}
	}

	candidates := []string{trimmed}
	if strings.Contains(trimmed, "-") {
		base := strings.SplitN(trimmed, "-", 2)[0]
		if base != trimmed {
			candidates = append(candidates, base)
		}
	}
	if trimmed != "en" {
		candidates = append(candidates, "en")
	}
	return candidates
}

func localizedMenuMain(language, repoURL, title string, includeLanguageSwitch bool) []map[string]interface{} {
	var mainMenu []map[string]interface{}

	if strings.TrimSpace(title) != "" {
		mainMenu = append(mainMenu, map[string]interface{}{
			"name":    strings.TrimSpace(title),
			"pageRef": "/",
			"weight":  1,
		})
	}

	mainMenu = append(mainMenu, []map[string]interface{}{
		{
			"name":    localizedString(language, "profiles", "Profiles"),
			"pageRef": "/profiles",
			"weight":  4,
		},
		{
			"name":   localizedString(language, "search", "Search"),
			"weight": 100,
			"params": map[string]string{"type": "search"},
		},
		{
			"name":   localizedString(language, "theme", "Theme"),
			"weight": 110,
			"params": map[string]string{"type": "theme-toggle"},
		},
	}...)
	if includeLanguageSwitch {
		mainMenu = append(mainMenu, map[string]interface{}{
			"name":   localizedString(language, "language", "Language"),
			"weight": 115,
			"params": map[string]string{"type": "language-switch"},
		})
	}

	if repoURL != "" {
		mainMenu = append(mainMenu, map[string]interface{}{
			"name":   localizedString(language, "github", "GitHub"),
			"url":    repoURL,
			"weight": 120,
			"params": map[string]string{"icon": "github"},
		})
	}

	return mainMenu
}

func localizedLanguagesConfig(languages map[string]interface{}, repoURL, title string, includeLanguageSwitch bool) map[string]interface{} {
	localized := make(map[string]interface{}, len(languages))
	for language, rawConfig := range languages {
		entry, ok := rawConfig.(map[string]interface{})
		if !ok {
			localized[language] = rawConfig
			continue
		}

		clone := make(map[string]interface{}, len(entry)+1)
		for key, value := range entry {
			clone[key] = value
		}
		clone["menu"] = map[string]interface{}{
			"main": localizedMenuMain(language, repoURL, title, includeLanguageSwitch),
		}
		localized[language] = clone
	}
	return localized
}

func buildVarLookup(m *compose.Project) map[string]compose.Var {
	lookup := make(map[string]compose.Var)
	for _, v := range m.AllVars() {
		lookup[v.Key] = v
	}
	return lookup
}

func generatedPageLanguages(m *compose.Project) (string, []string, error) {
	lookup := buildVarLookup(m)
	defaultLanguage := hugoConfigValue(m.Meta.HugoDefaultLanguage, lookup, "HUGO_DEFAULT_CONTENT_LANGUAGE")
	defaultLanguage = strings.TrimSpace(defaultLanguage)
	if defaultLanguage == "" {
		defaultLanguage = "en"
	}

	rawLanguages := hugoConfigValue(m.Meta.HugoLanguages, lookup, "HUGO_LANGUAGES")
	if strings.TrimSpace(rawLanguages) == "" {
		return defaultLanguage, nil, nil
	}

	languages, err := parseHugoLanguages(rawLanguages)
	if err != nil {
		return "", nil, fmt.Errorf("parsing HUGO_LANGUAGES for generated pages: %w", err)
	}

	additionalLanguages := make([]string, 0, len(languages))
	for language := range languages {
		language = strings.TrimSpace(language)
		if language == "" || language == defaultLanguage {
			continue
		}
		additionalLanguages = append(additionalLanguages, language)
	}
	sort.Strings(additionalLanguages)

	return defaultLanguage, additionalLanguages, nil
}

func generatedPageString(language, key string) string {
	switch language {
	case "de":
		switch key {
		case "setsTitle":
			return localizedString(language, "sets", "Sets")
		case "setsDescription":
			return localizedString(language, "generatedSetsDescription", "Automatisch generierte Referenz der Konfigurations-Sets aus compose.yml.")
		case "servicesTitle":
			return localizedString(language, "services", "Dienste")
		case "servicesDescription":
			return localizedString(language, "generatedServicesDescription", "Automatisch generierte Dienstreferenz aus compose.yml.")
		case "profilesTitle":
			return localizedString(language, "profiles", "Profile")
		case "profilesDescription":
			return localizedString(language, "generatedProfilesDescription", "Automatisch generierte Profilreferenz aus compose.yml.")
		case "profileNoVariables":
			return localizedString(language, "profileNoVariables", "Es wurden keine Profile definiert.")
		case "setDescriptionFallback":
			return localizedString(language, "setConfiguration", "Set-Konfiguration")
		case "setNoVariables":
			return localizedString(language, "setNoVariables", "Dieses Set definiert keine Variablen.")
		case "serviceNoVariables":
			return localizedString(language, "serviceNoVariables", "Dieser Dienst hat keine definierten Variablen.")
		case "required":
			return localizedString(language, "required", "Erforderlich")
		case "defaultHidden":
			return localizedString(language, "defaultHidden", "Standardwert verborgen")
		case "example":
			return localizedString(language, "example", "Beispiel")
		case "secret":
			return localizedString(language, "secret", "Geheim")
		case "readonly":
			return localizedString(language, "readonly", "Schreibgeschuetzt")
		case "image":
			return localizedString(language, "image", "Image")
		case "platform":
			return localizedString(language, "platform", "Plattform")
		case "command":
			return localizedString(language, "command", "Befehl")
		case "readme":
			return localizedString(language, "readme", "Mehr Info")
		}
	default:
		switch key {
		case "setsTitle":
			return localizedString(language, "sets", "Sets")
		case "setsDescription":
			return localizedString(language, "generatedSetsDescription", "Auto-generated configuration set reference from compose.yml.")
		case "servicesTitle":
			return localizedString(language, "services", "Services")
		case "servicesDescription":
			return localizedString(language, "generatedServicesDescription", "Auto-generated service reference from compose.yml.")
		case "profilesTitle":
			return localizedString(language, "profiles", "Profiles")
		case "profilesDescription":
			return localizedString(language, "generatedProfilesDescription", "Auto-generated profile reference from compose.yml.")
		case "setDescriptionFallback":
			return localizedString(language, "setConfiguration", "Set configuration")
		case "setNoVariables":
			return localizedString(language, "setNoVariables", "This set does not define variables.")
		case "serviceNoVariables":
			return localizedString(language, "serviceNoVariables", "This service has no defined variables.")
		case "required":
			return localizedString(language, "required", "Required")
		case "defaultHidden":
			return localizedString(language, "defaultHidden", "Default hidden")
		case "example":
			return localizedString(language, "example", "Example")
		case "secret":
			return localizedString(language, "secret", "secret")
		case "readonly":
			return localizedString(language, "readonly", "readonly")
		case "image":
			return localizedString(language, "image", "Image")
		case "platform":
			return localizedString(language, "platform", "Platform")
		case "command":
			return localizedString(language, "command", "Command")
		case "readme":
			return localizedString(language, "readme", "More info")
		}
	}

	return ""
}

func detectRepositoryURL(dir string) string {
	cmd := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	url := strings.TrimSpace(string(out))
	if url == "" {
		return ""
	}

	return normalizeRepositoryURL(url)
}

func normalizeRepositoryURL(raw string) string {
	url := strings.TrimSpace(raw)
	if url == "" {
		return ""
	}

	url = strings.TrimSuffix(url, ".git")

	if strings.HasPrefix(url, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(url, "git@"), ":", 2)
		if len(parts) == 2 {
			return "https://" + parts[0] + "/" + parts[1]
		}
	}

	if strings.HasPrefix(url, "ssh://git@") {
		trimmed := strings.TrimPrefix(url, "ssh://git@")
		if i := strings.Index(trimmed, "/"); i > 0 {
			host := trimmed[:i]
			path := trimmed[i+1:]
			return "https://" + host + "/" + path
		}
	}

	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return url
	}

	return ""
}

const persistentContentDirName = ".envy"

func prepareBuildContentDir(siteRoot string, m *compose.Project) (string, error) {
	return prepareBuildContentDirWithOptions(siteRoot, m, true)
}

func prepareBuildContentDirWithOptions(siteRoot string, m *compose.Project, refresh bool) (string, error) {
	contentDir := filepath.Join(siteRoot, persistentContentDirName)
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		return "", fmt.Errorf("creating persistent Hugo content directory: %w", err)
	}
	if refresh {
		if err := refreshPersistentContentDir(contentDir); err != nil {
			return "", err
		}
	}

	defaultLanguage, additionalLanguages, err := generatedPageLanguages(m)
	if err != nil {
		return "", err
	}

	if err := writeLocalizedHomePage(siteRoot, filepath.Join(contentDir, "_index.md"), defaultLanguage, generateHomeMarkdown(m)); err != nil {
		return "", err
	}
	for _, language := range additionalLanguages {
		if err := writeLocalizedHomePage(siteRoot, filepath.Join(contentDir, "_index."+language+".md"), language, generateHomeMarkdown(m)); err != nil {
			return "", err
		}
	}

	setsDir := filepath.Join(contentDir, "sets")
	if err := os.MkdirAll(setsDir, 0o755); err != nil {
		return "", fmt.Errorf("creating generated sets directory: %w", err)
	}

	if err := writeGeneratedFile(contentDir, filepath.Join(setsDir, "_index.md"), generateSetsIndexMarkdown(m, defaultLanguage)); err != nil {
		return "", err
	}
	for _, language := range additionalLanguages {
		if err := writeGeneratedFile(contentDir, filepath.Join(setsDir, "_index."+language+".md"), generateSetsIndexMarkdown(m, language)); err != nil {
			return "", err
		}
	}

	for _, set := range m.OrderedSets() {
		pagePath := filepath.Join(setsDir, sanitizeSetPageName(set.Key)+".md")
		if err := writeGeneratedFile(contentDir, pagePath, generateSetMarkdown(m, set, defaultLanguage)); err != nil {
			return "", err
		}
		for _, language := range additionalLanguages {
			localizedPagePath := filepath.Join(setsDir, sanitizeSetPageName(set.Key)+"."+language+".md")
			if err := writeGeneratedFile(contentDir, localizedPagePath, generateSetMarkdown(m, set, language)); err != nil {
				return "", err
			}
		}
	}

	if len(m.Services) > 0 {
		servicesDir := filepath.Join(contentDir, "services")
		if err := os.MkdirAll(servicesDir, 0o755); err != nil {
			return "", fmt.Errorf("creating generated services directory: %w", err)
		}
		if err := writeGeneratedFile(contentDir, filepath.Join(servicesDir, "_index.md"), generateServicesIndexMarkdown(m, defaultLanguage)); err != nil {
			return "", err
		}
		for _, language := range additionalLanguages {
			if err := writeGeneratedFile(contentDir, filepath.Join(servicesDir, "_index."+language+".md"), generateServicesIndexMarkdown(m, language)); err != nil {
				return "", err
			}
		}

		for _, service := range m.Services {
			pagePath := filepath.Join(servicesDir, sanitizeServicePageName(service.Name)+".md")
			if err := writeGeneratedFile(contentDir, pagePath, generateServiceMarkdown(m, service, defaultLanguage)); err != nil {
				return "", err
			}
			for _, language := range additionalLanguages {
				localizedPagePath := filepath.Join(servicesDir, sanitizeServicePageName(service.Name)+"."+language+".md")
				if err := writeGeneratedFile(contentDir, localizedPagePath, generateServiceMarkdown(m, service, language)); err != nil {
					return "", err
				}
			}
		}
	}

	profilesDir := filepath.Join(contentDir, "profiles")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		return "", fmt.Errorf("creating generated profiles directory: %w", err)
	}
	if err := writeGeneratedFile(contentDir, filepath.Join(profilesDir, "_index.md"), generateProfilesIndexMarkdown(m, defaultLanguage)); err != nil {
		return "", err
	}
	for _, language := range additionalLanguages {
		if err := writeGeneratedFile(contentDir, filepath.Join(profilesDir, "_index."+language+".md"), generateProfilesIndexMarkdown(m, language)); err != nil {
			return "", err
		}
	}

	profiles := append([]string{"none"}, projectProfiles(m)...)
	for _, profile := range profiles {
		if strings.TrimSpace(profile) == "" {
			continue
		}
		pagePath := filepath.Join(profilesDir, sanitizeProfilePageName(profile)+".md")
		if err := writeGeneratedFile(contentDir, pagePath, generateProfileMarkdown(m, profile, defaultLanguage)); err != nil {
			return "", err
		}
		for _, language := range additionalLanguages {
			localizedPagePath := filepath.Join(profilesDir, sanitizeProfilePageName(profile)+"."+language+".md")
			if err := writeGeneratedFile(contentDir, localizedPagePath, generateProfileMarkdown(m, profile, language)); err != nil {
				return "", err
			}
		}
	}

	return contentDir, nil
}

func refreshPersistentContentDir(contentDir string) error {
	entries, err := os.ReadDir(contentDir)
	if err != nil {
		return fmt.Errorf("reading persistent Hugo content directory %s: %w", contentDir, err)
	}

	for _, entry := range entries {
		if entry.Name() == ".gitkeep" {
			continue
		}

		targetPath := filepath.Join(contentDir, entry.Name())
		if err := os.RemoveAll(targetPath); err != nil {
			return fmt.Errorf("removing stale Hugo content %s: %w", targetPath, err)
		}
	}

	return nil
}

func writeLocalizedHomePage(siteRoot, dst, language, fallback string) error {
	if language != "" {
		localizedReadme := filepath.Join(siteRoot, "README."+language+".md")
		if _, err := os.Stat(localizedReadme); err == nil {
			return copyFile(localizedReadme, dst)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("checking localized README %s: %w", localizedReadme, err)
		}
	}

	defaultReadme := filepath.Join(siteRoot, "README.md")
	if _, err := os.Stat(defaultReadme); err == nil {
		return copyFile(defaultReadme, dst)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking default README %s: %w", defaultReadme, err)
	}

	return writeGeneratedFile("", dst, fallback)
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

func writeGeneratedFile(contentDir, path, content string) error {
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

func generateSetsIndexMarkdown(m *compose.Project, language string) string {
	var body strings.Builder
	title := generatedPageString(language, "setsTitle")
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       title,
		"description": generatedPageString(language, "setsDescription"),
		"weight":      10,
		"sidebar": map[string]interface{}{
			"hide": true,
		},
		"menu": map[string]interface{}{
			"main": map[string]interface{}{
				"name":   title,
				"weight": 10,
			},
		},
	}))
	body.WriteString(renderCardsOpen(2))
	for _, set := range m.OrderedSets() {
		services := servicesForSet(m, set.Key)
		body.WriteString(renderSetOverviewCard(set, services, language))
	}
	body.WriteString(renderCardsClose())
	return body.String()
}

func generateServicesIndexMarkdown(m *compose.Project, language string) string {
	var body strings.Builder
	title := generatedPageString(language, "servicesTitle")
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       title,
		"description": generatedPageString(language, "servicesDescription"),
		"weight":      5,
		"sidebar": map[string]interface{}{
			"hide": true,
		},
		"menu": map[string]interface{}{
			"main": map[string]interface{}{
				"name":   title,
				"weight": 5,
			},
		},
	}))
	body.WriteString(renderCardsOpen(2))
	for _, service := range m.Services {
		body.WriteString(renderServiceOverviewCard(service, language))
	}
	body.WriteString(renderCardsClose())
	return body.String()
}

func generateProfilesIndexMarkdown(m *compose.Project, language string) string {
	var body strings.Builder
	title := generatedPageString(language, "profilesTitle")
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       title,
		"description": generatedPageString(language, "profilesDescription"),
		"weight":      6,
		"sidebar": map[string]interface{}{
			"hide": true,
		},
	}))

	profiles := projectProfiles(m)

	body.WriteString(renderCardsOpen(3))
	body.WriteString(renderProfileCard("none", "/profiles/none/"))
	for _, profile := range profiles {
		if strings.TrimSpace(profile) == "none" {
			continue
		}
		body.WriteString(renderProfileCard(profile, "/profiles/"+sanitizeProfilePageName(profile)+"/"))
	}
	body.WriteString(renderCardsClose())
	return body.String()
}

func generateProfileMarkdown(m *compose.Project, profile, language string) string {
	var body strings.Builder
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":     profile,
		"weight":    profileWeight(m, profile),
		"hideTitle": true,
		"toc":       false,
	}))

	// Show the selected profile at the top in a full-width card.
	body.WriteString(renderCardsOpen(1))
	body.WriteString(renderCardWithTagIconOptions(profile, "", "profile", "", "", "", "", "", "", "", "", ""))
	body.WriteString(renderCardsClose())
	body.WriteString("\n")

	services := servicesForProfile(m, profile)
	if strings.TrimSpace(profile) == "none" {
		services = servicesWithoutProfiles(m)
	} else {
		// For regular profiles, also include services without any profiles
		servicesWithout := servicesWithoutProfiles(m)
		services = append(services, servicesWithout...)
	}
	if len(services) == 0 {
		return body.String()
	}

	body.WriteString(renderCardsOpen(2))
	for _, service := range services {
		body.WriteString(renderServiceOverviewCard(service, language))
	}
	body.WriteString(renderCardsClose())
	return body.String()
}

func generateServiceMarkdown(m *compose.Project, service compose.Service, language string) string {
	var body strings.Builder
	metaDescription := strings.Join(strings.Fields(strings.TrimSpace(service.Description)), " ")
	body.WriteString(frontMatterMarkdown(map[string]interface{}{
		"title":       service.Name,
		"description": metaDescription,
		"weight":      serviceWeight(m, service.Name),
		"hideTitle":   true,
		"toc":         false,
	}))

	body.WriteString(renderCardsOpen(1))
	body.WriteString(renderServiceDetailCard(service, language))
	body.WriteString(renderCardsClose())
	body.WriteString("\n")

	vars := varsForServiceSorted(m, service)
	if len(vars) == 0 {
		body.WriteString(renderInfoCallout(generatedPageString(language, "serviceNoVariables")))
		return body.String()
	}

	for _, variable := range vars {
		body.WriteString(fmt.Sprintf("<div id=\"%s\"></div>\n\n", variableHeadingAnchor(variable.Key)))
		body.WriteString(renderCardsOpen(1))
		body.WriteString(renderVarCard(variable, variableCardSubtitle(variable, language), variableCardClass(variable), true, language))
		body.WriteString(renderCardsClose())
		body.WriteString("\n")
	}

	return body.String()
}

func generateSetMarkdown(m *compose.Project, set compose.Set, language string) string {
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
	body.WriteString(renderSetCard(set, services, language))
	body.WriteString(renderCardsClose())
	body.WriteString("\n")

	if len(set.Vars) == 0 {
		body.WriteString(renderInfoCallout(generatedPageString(language, "setNoVariables")))
		return body.String()
	}
	for _, variable := range set.Vars {
		body.WriteString(fmt.Sprintf("<div id=\"%s\"></div>\n\n", variableHeadingAnchor(variable.Key)))
		body.WriteString(renderCardsOpen(1))
		body.WriteString(renderVarCard(variable, variableCardSubtitle(variable, language), variableCardClass(variable), true, language))
		body.WriteString(renderCardsClose())
		body.WriteString("\n")
	}

	return body.String()
}

func renderServiceOverviewCard(service compose.Service, language string) string {
	return renderServiceCard(service, language, "/services/"+sanitizeServicePageName(service.Name))
}

func renderServiceDetailCard(service compose.Service, language string) string {
	return renderServiceCard(service, language, "")
}

func renderServiceCard(service compose.Service, language string, titleLink string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card title=\"%s\"",
		escapeShortcodeValue(service.Name),
	))
	if strings.TrimSpace(titleLink) != "" {
		sb.WriteString(fmt.Sprintf(" titleLink=\"%s\"", escapeShortcodeValue(titleLink)))
	}
	sb.WriteString(" cardType=\"service\"")

	description, descriptionLink := splitServiceDescriptionAndLink(service.Description)
	if description != "" {
		sb.WriteString(fmt.Sprintf(" description=`%s`", escapeShortcodeRawValue(description)))
	}
	if descriptionLink != "" {
		sb.WriteString(fmt.Sprintf(" descriptionLink=\"%s\"", escapeShortcodeValue(descriptionLink)))
	}

	if strings.TrimSpace(service.Image) != "" {
		imageValue := strings.TrimSpace(service.Image)
		sb.WriteString(fmt.Sprintf(" dockerImage=\"%s\"", escapeShortcodeValue(imageValue)))
		imageLink := imageLink(imageValue)
		if imageLink != "" {
			sb.WriteString(fmt.Sprintf(" dockerImageLink=\"%s\"", escapeShortcodeValue(imageLink)))
		}
	}

	if platform := strings.TrimSpace(service.Platform); platform != "" {
		sb.WriteString(fmt.Sprintf(" platform=\"%s\"", escapeShortcodeValue(platform)))
	}

	if len(service.Command) > 0 {
		rawCommand := composeCommandLiteral(service.Command)
		sb.WriteString(fmt.Sprintf(" command=\"%s\"", escapeShortcodeValue(rawCommand)))
	}

	if setsValue := csvAttributeValues(service.Sets); setsValue != "" {
		sb.WriteString(fmt.Sprintf(" tagsSets=\"%s\"", escapeShortcodeValue(setsValue)))
	}

	if profilesValue := csvAttributeValues(service.Profiles); profilesValue != "" {
		sb.WriteString(fmt.Sprintf(" tagsProfiles=\"%s\"", escapeShortcodeValue(profilesValue)))
	}

	sb.WriteString(" >}}\n")
	return sb.String()
}

func splitServiceDescriptionAndLink(raw string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}

	lines := strings.Split(trimmed, "\n")
	keptLines := make([]string, 0, len(lines))
	link := ""

	for _, line := range lines {
		clean := strings.TrimSpace(line)
		if clean == "" {
			keptLines = append(keptLines, line)
			continue
		}

		if link == "" {
			if extracted := extractServiceDescriptionLink(clean); extracted != "" {
				link = extracted
				continue
			}
		}

		keptLines = append(keptLines, line)
	}

	return strings.TrimSpace(strings.Join(keptLines, "\n")), link
}

func extractServiceDescriptionLink(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "link:") {
		candidate := strings.TrimSpace(trimmed[len("link:"):])
		if strings.HasPrefix(candidate, "https://") || strings.HasPrefix(candidate, "http://") {
			return candidate
		}
		return ""
	}

	if strings.HasPrefix(trimmed, "[") {
		if end := strings.Index(trimmed, "]:"); end > 1 {
			candidate := strings.TrimSpace(trimmed[end+2:])
			if strings.HasPrefix(candidate, "https://") || strings.HasPrefix(candidate, "http://") {
				return candidate
			}
		}

		if open := strings.Index(trimmed, "]("); open > 1 && strings.HasSuffix(trimmed, ")") {
			candidate := strings.TrimSpace(trimmed[open+2 : len(trimmed)-1])
			if strings.HasPrefix(candidate, "https://") || strings.HasPrefix(candidate, "http://") {
				return candidate
			}
		}
	}

	if (strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "http://")) && !strings.Contains(trimmed, " ") {
		return trimmed
	}

	return ""
}

func csvAttributeValues(values []string) string {
	if len(values) == 0 {
		return ""
	}

	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}

	if len(cleaned) == 0 {
		return ""
	}

	return strings.Join(cleaned, ",")
}

func renderServiceSetsBadges(service compose.Service) string {
	if len(service.Sets) == 0 {
		return ""
	}

	badges := make([]string, 0, len(service.Sets))
	seenSets := make(map[string]struct{}, len(service.Sets))
	for _, setKey := range service.Sets {
		trimmedSet := strings.TrimSpace(setKey)
		if trimmedSet == "" {
			continue
		}
		if _, exists := seenSets[trimmedSet]; exists {
			continue
		}
		seenSets[trimmedSet] = struct{}{}
		badges = append(badges, renderLinkedHextraBadgeHTMLWithSize(trimmedSet, "/sets/"+sanitizeSetPageName(trimmedSet)+"/", "blue", false, "hx:text-xs"))
	}

	if len(badges) == 0 {
		return ""
	}

	return `<div class="not-prose hx:flex hx:flex-wrap hx:gap-2 hx:mb-4">` + strings.Join(badges, "") + `</div>`
}

func renderServiceProfilesBadges(service compose.Service) string {
	if len(service.Profiles) == 0 {
		return ""
	}

	badges := make([]string, 0, len(service.Profiles))
	seenProfiles := make(map[string]struct{}, len(service.Profiles))
	for _, profile := range service.Profiles {
		trimmedProfile := strings.TrimSpace(profile)
		if trimmedProfile == "" {
			continue
		}
		if _, exists := seenProfiles[trimmedProfile]; exists {
			continue
		}
		seenProfiles[trimmedProfile] = struct{}{}
		badges = append(badges, renderLinkedHextraBadgeHTMLWithSize(trimmedProfile, "/profiles/", "green", false, "hx:text-xs"))
	}

	if len(badges) == 0 {
		return ""
	}

	return `<div class="not-prose hx:flex hx:flex-wrap hx:gap-2 hx:mb-4">` + strings.Join(badges, "") + `</div>`
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

// composeCommandLiteral formats a Docker Compose list-form command value.
// Example: [ "valkey-server", "--loglevel", "warning" ]
func composeCommandLiteral(args []string) string {
	if len(args) == 0 {
		return ""
	}
	formatted := make([]string, 0, len(args))
	for _, arg := range args {
		escapedArg := strings.ReplaceAll(arg, "\\", "\\\\")
		escapedArg = strings.ReplaceAll(escapedArg, "\"", "\\\"")
		formatted = append(formatted, fmt.Sprintf("\"%s\"", escapedArg))
	}

	return "[ " + strings.Join(formatted, ", ") + " ]"
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

func sanitizeServicePageName(serviceName string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	return replacer.Replace(serviceName)
}

func sanitizeProfilePageName(profileName string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	return replacer.Replace(profileName)
}

func setWeight(m *compose.Project, setKey string) int {
	for index, set := range m.OrderedSets() {
		if set.Key == setKey {
			return index + 1
		}
	}
	return 999
}

func serviceWeight(m *compose.Project, serviceName string) int {
	for index, service := range m.Services {
		if service.Name == serviceName {
			return index + 1
		}
	}
	return 999
}

func profileWeight(m *compose.Project, profileName string) int {
	if strings.TrimSpace(profileName) == "none" {
		return 1
	}
	for index, profile := range projectProfiles(m) {
		if profile == profileName {
			return index + 2
		}
	}
	return 999
}

func varsForServiceSorted(m *compose.Project, service compose.Service) []compose.Var {
	vars := make([]compose.Var, 0)
	for _, setKey := range service.Sets {
		set, ok := m.Sets[setKey]
		if !ok {
			continue
		}
		vars = append(vars, set.Vars...)
	}

	sort.SliceStable(vars, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(vars[i].Key))
		right := strings.ToLower(strings.TrimSpace(vars[j].Key))
		if left == right {
			return vars[i].Key < vars[j].Key
		}
		return left < right
	})

	return vars
}

func projectProfiles(m *compose.Project) []string {
	if m == nil || len(m.Services) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	for _, service := range m.Services {
		for _, profile := range service.Profiles {
			trimmed := strings.TrimSpace(profile)
			if trimmed == "" {
				continue
			}
			seen[trimmed] = struct{}{}
		}
	}

	profiles := make([]string, 0, len(seen))
	for profile := range seen {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)
	return profiles
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

func renderInfoCallout(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return ""
	}

	return fmt.Sprintf("{{< callout type=\"info\" >}}\n%s\n{{< /callout >}}\n", trimmed)
}

func renderCard(title, link, icon, description string) string {
	return renderCardWithTag(title, link, icon, description, "", "", "", "", "", "")
}

func renderProfileCard(title, link string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card link=\"%s\" title=\"%s\" cardType=\"profile\" >}}\n",
		escapeShortcodeValue(link),
		escapeShortcodeValue(title),
	))
	return sb.String()
}

func renderCardWithTag(title, link, icon, description, htmlContent, titlePadding, tag, tagColor, tagBorder, class string) string {
	return renderCardWithTagIconOptions(title, link, icon, description, htmlContent, titlePadding, tag, tagColor, tagBorder, class, "", "")
}

func renderCardWithTagIconOptions(title, link, icon, description, htmlContent, titlePadding, tag, tagColor, tagBorder, class, iconAttributes, iconGapClass string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card link=\"%s\" title=\"%s\"",
		escapeShortcodeValue(link),
		escapeShortcodeValue(title),
	))
	if icon == "profile" || icon == "service" || icon == "set" || icon == "var" {
		sb.WriteString(fmt.Sprintf(" cardType=\"%s\"", escapeShortcodeValue(icon)))
	} else if icon != "" {
		sb.WriteString(fmt.Sprintf(" icon=\"%s\"", escapeShortcodeValue(icon)))
	}
	if strings.TrimSpace(description) != "" {
		sb.WriteString(fmt.Sprintf(" description=`%s`", escapeShortcodeRawValue(description)))
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
	if iconAttributes != "" {
		sb.WriteString(fmt.Sprintf(" iconAttributes=\"%s\"", escapeShortcodeValue(iconAttributes)))
	}
	if iconGapClass != "" {
		sb.WriteString(fmt.Sprintf(" iconGapClass=\"%s\"", escapeShortcodeValue(iconGapClass)))
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

func renderSetCard(set compose.Set, services []string, language string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card title=\"%s\" cardType=\"set\"",
		escapeShortcodeValue(set.Key),
	))

	// Add description.
	if strings.TrimSpace(set.Description) != "" {
		sb.WriteString(fmt.Sprintf(" description=`%s`", escapeShortcodeRawValue(strings.TrimSpace(set.Description))))
	}

	// Add documentation link.
	if strings.TrimSpace(set.Link) != "" {
		_, linkTarget := normalizeSetDocLink(set.Link)
		sb.WriteString(fmt.Sprintf(" descriptionLink=\"%s\"", escapeShortcodeValue(linkTarget)))
	}

	if servicesValue := csvAttributeValues(services); servicesValue != "" {
		sb.WriteString(fmt.Sprintf(" tagsServices=\"%s\"", escapeShortcodeValue(servicesValue)))
	}

	sb.WriteString(" >}}\n")
	return sb.String()
}

func renderSetOverviewCard(set compose.Set, services []string, language string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card title=\"%s\" titleLink=\"%s\" cardType=\"set\"",
		escapeShortcodeValue(set.Key),
		escapeShortcodeValue("/sets/"+sanitizeSetPageName(set.Key)+"/"),
	))

	description := strings.TrimSpace(defaultString(set.Description, generatedPageString(language, "setDescriptionFallback")))
	if description != "" {
		sb.WriteString(fmt.Sprintf(" description=`%s`", escapeShortcodeRawValue(description)))
	}

	if strings.TrimSpace(set.Link) != "" {
		_, linkTarget := normalizeSetDocLink(set.Link)
		sb.WriteString(fmt.Sprintf(" descriptionLink=\"%s\"", escapeShortcodeValue(linkTarget)))
	}

	if servicesValue := csvAttributeValues(services); servicesValue != "" {
		sb.WriteString(fmt.Sprintf(" tagsServices=\"%s\"", escapeShortcodeValue(servicesValue)))
	}

	sb.WriteString(" >}}\n")
	return sb.String()
}

func renderLinkedHextraBadgeHTML(label, href, color string, border bool) string {
	return renderLinkedHextraBadgeHTMLWithSize(label, href, color, border, "hx:text-[.65rem]")
}

func renderLinkedHextraBadgeHTMLWithSize(label, href, color string, border bool, sizeClass string) string {
	badgeClass := hextraBadgeColorClass(color)
	borderClass := ""
	if border {
		borderClass = "hx:border "
	}
	if strings.TrimSpace(sizeClass) == "" {
		sizeClass = "hx:text-[.65rem]"
	}

	return fmt.Sprintf(
		"<a href=\"%s\" title=\"%s\" class=\"not-prose hx:inline-flex hx:align-middle hx:no-underline hover:hx:no-underline\"><div class=\"hextra-badge\"><div class=\"hx:inline-flex hx:gap-1 hx:items-center hx:rounded-full hx:px-2.5 hx:leading-6 %s %s%s\">%s</div></div></a>",
		html.EscapeString(href),
		html.EscapeString(label),
		sizeClass,
		borderClass,
		badgeClass,
		html.EscapeString(label),
	)
}

func normalizeSetDocLink(raw string) (label string, target string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	if strings.HasPrefix(raw, "[") {
		if end := strings.Index(raw, "]:"); end > 1 {
			text := strings.TrimSpace(raw[1:end])
			url := strings.TrimSpace(raw[end+2:])
			if text != "" && url != "" {
				return text, url
			}
		}
	}

	return raw, raw
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

func renderVarCard(variable compose.Var, description, class string, showMissingAsSecret bool, language string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card link=\"\" title=\"%s\" cardType=\"var\"",
		escapeShortcodeValue(variable.Key),
	))
	if strings.TrimSpace(description) != "" {
		sb.WriteString(fmt.Sprintf(" description=`%s`", escapeShortcodeRawValue(description)))
	}
	if strings.TrimSpace(variable.Link) != "" {
		sb.WriteString(fmt.Sprintf(" descriptionLink=\"%s\"", escapeShortcodeValue(strings.TrimSpace(variable.Link))))
	}
	defaultValue := strings.TrimSpace(variable.DefaultString())
	hasVarValue := false
	hasQuestionPrefix := showMissingAsSecret && strings.HasPrefix(defaultValue, "?")
	if hasQuestionPrefix {
		defaultValue = strings.TrimPrefix(defaultValue, "?")
	}
	if !variable.IsSecret() && defaultValue != "" {
		if variable.IsReadonly() {
			sb.WriteString(fmt.Sprintf(" var=\"%s\"", escapeShortcodeValue("envy:readonly:"+defaultValue)))
		} else {
			sb.WriteString(fmt.Sprintf(" var=\"%s\"", escapeShortcodeValue(defaultValue)))
		}
		hasVarValue = true
	}
	if hasQuestionPrefix {
		sb.WriteString(fmt.Sprintf(" tagBottom=\"%s\" tagBottomColor=\"red\"", escapeShortcodeValue(generatedPageString(language, "required"))))
	}
	if showMissingAsSecret && !hasVarValue {
		sb.WriteString(fmt.Sprintf(" tagBottom=\"%s\" tagBottomColor=\"orange\"", escapeShortcodeValue(generatedPageString(language, "secret"))))
	}
	if class != "" {
		sb.WriteString(fmt.Sprintf(" class=\"%s\"", escapeShortcodeValue(class)))
	}
	sb.WriteString(" >}}\n")
	return sb.String()
}

func variableCardTag(variable compose.Var, language string) (string, string, string) {
	if variable.IsSecret() {
		return generatedPageString(language, "secret"), "orange", "false"
	}
	if variable.IsReadonly() {
		return generatedPageString(language, "readonly"), "yellow", "false"
	}
	return "", "", ""
}

func variableCardClass(variable compose.Var) string {
	if variable.IsRequired() {
		return "[&:user-invalid]:hx:border-red-500 [&:user-invalid]:hx:bg-red-50 [&:user-invalid]:hx:dark:bg-red-900/20"
	}
	return ""
}

func variableCardSubtitle(variable compose.Var, language string) string {
	parts := make([]string, 0, 5)
	if strings.TrimSpace(variable.Description) != "" {
		parts = append(parts, strings.TrimSpace(variable.Description))
	}

	meta := make([]string, 0, 2)
	if variable.IsRequired() {
		meta = append(meta, generatedPageString(language, "required"))
	}
	if len(meta) > 0 {
		parts = append(parts, strings.Join(meta, " · "))
	}

	defaultValue := strings.TrimSpace(variable.DefaultString())
	if defaultValue != "" {
		if variable.IsSecret() {
			parts = append(parts, generatedPageString(language, "defaultHidden"))
		}
	}

	if strings.TrimSpace(variable.Example) != "" {
		parts = append(parts, generatedPageString(language, "example")+": `"+strings.TrimSpace(variable.Example)+"`")
	}
	if len(parts) == 0 {
		if defaultValue != "" || variable.IsSecret() {
			return ""
		}
	}
	return strings.Join(parts, "\n\n")
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

func servicesForProfile(m *compose.Project, profile string) []compose.Service {
	if m == nil || len(m.Services) == 0 {
		return nil
	}

	trimmedProfile := strings.TrimSpace(profile)
	if trimmedProfile == "" {
		return nil
	}

	services := make([]compose.Service, 0, len(m.Services))
	for _, service := range m.Services {
		for _, candidate := range service.Profiles {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if candidate == trimmedProfile {
				services = append(services, service)
				break
			}
		}
	}

	return services
}

func servicesWithoutProfiles(m *compose.Project) []compose.Service {
	if m == nil || len(m.Services) == 0 {
		return nil
	}

	services := make([]compose.Service, 0, len(m.Services))
	for _, service := range m.Services {
		hasProfile := false
		for _, candidate := range service.Profiles {
			if strings.TrimSpace(candidate) != "" {
				hasProfile = true
				break
			}
		}
		if !hasProfile {
			services = append(services, service)
		}
	}

	return services
}
