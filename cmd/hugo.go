package cmd

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
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

const envyWatchProxyBind = "127.0.0.1:1313"
const envyInternalHugoBind = "127.0.0.1"
const envyInternalHugoPort = "1314"
const envyWatchPublicURL = "http://localhost:1313"
const internalHugoServerMessage = "Web Server is available at //localhost:1314/ (bind address 127.0.0.1)"
const publicEnvyServerMessage = "Web Server is available at //localhost:1313/ (bind address 127.0.0.1)"
const hugoFastRenderMessage = "Running in Fast Render Mode. For full rebuilds on change: hugo server --disableFastRender"
const envyFastRenderMessage = "Running in Fast Render Mode. For full rebuilds on change: envy server --disableFastRender"

var composeInterpolationPatternLocal = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-?])(.*))?\}$`)
var editorPrefixedLinkPattern = regexp.MustCompile(`(?i)^link:\s*(https?://\S+)$`)
var editorPlainLinkPattern = regexp.MustCompile(`^https?://\S+$`)

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

		buildSiteDir, err = prepareBuildAssets(manifestFilePath, refreshPersistentContent, "")
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

	hugoArgs := make([]string, 0, len(args)+5)
	hugoArgs = append(hugoArgs, "server", "--bind", envyInternalHugoBind, "--port", envyInternalHugoPort)
	hugoArgs = append(hugoArgs, filterListenFlags(args)...)

	buildSiteDir, err := prepareBuildAssets(manifestFilePath, false, envyWatchPublicURL)
	if err != nil {
		return err
	}
	defer os.RemoveAll(buildSiteDir)

	command, _ := buildHugoExecCommand(hugoArgs)
	command.Stdin = os.Stdin
	command.Stdout = &messageRewriteWriter{target: os.Stdout}
	command.Stderr = &messageRewriteWriter{target: os.Stderr}
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

	hugoTargetURL, err := url.Parse("http://" + envyInternalHugoBind + ":" + envyInternalHugoPort)
	if err != nil {
		return fmt.Errorf("parsing internal hugo url: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(hugoTargetURL)
	mux := http.NewServeMux()
	var syncBuildMu sync.Mutex
	syncGeneratedSite := func() error {
		syncBuildMu.Lock()
		defer syncBuildMu.Unlock()
		return syncBuildAssets(manifestFilePath, buildSiteDir, false, envyWatchPublicURL)
	}
	mux.HandleFunc("/api/vars", editorAPIHandler(manifestFilePath))
	mux.HandleFunc("/api/services", serviceAPIHandler(manifestFilePath, syncGeneratedSite))
	mux.HandleFunc("/api/services/", serviceAPIHandler(manifestFilePath, syncGeneratedSite))
	mux.HandleFunc("/api/sets", setAPIHandler(manifestFilePath, syncGeneratedSite))
	mux.HandleFunc("/api/sets/", setAPIHandler(manifestFilePath, syncGeneratedSite))
	mux.HandleFunc("/api/profiles", profileAPIHandler(manifestFilePath, syncGeneratedSite))
	mux.HandleFunc("/api/profiles/", profileAPIHandler(manifestFilePath, syncGeneratedSite))
	mux.Handle("/", proxy)

	frontendServer := &http.Server{Addr: envyWatchProxyBind, Handler: mux}
	frontendErrCh := make(chan error, 1)
	go func() {
		if err := frontendServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			frontendErrCh <- err
		}
	}()

	fileState, err := manifestState(manifestFilePath)
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
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = frontendServer.Shutdown(ctx)
			cancel()
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
		case frontErr := <-frontendErrCh:
			if command.Process != nil {
				_ = command.Process.Signal(os.Interrupt)
			}
			return fmt.Errorf("watch frontend server failed: %w", frontErr)
		case waitErr := <-waitCh:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = frontendServer.Shutdown(ctx)
			cancel()
			if waitErr != nil {
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					return fmt.Errorf("hugo server exited with code %d", exitErr.ExitCode())
				}
				return fmt.Errorf("running hugo server: %w", waitErr)
			}
			return nil
		case <-ticker.C:
			changed, nextState, stateErr := manifestChanged(manifestFilePath, fileState)
			if stateErr != nil {
				return stateErr
			}
			if !changed {
				continue
			}

			fmt.Fprintln(os.Stderr, "compose.yml changed, regenerating documentation content...")
			syncBuildMu.Lock()
			err := syncBuildAssets(manifestFilePath, buildSiteDir, false, envyWatchPublicURL)
			syncBuildMu.Unlock()
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to regenerate site assets: %v\n", err)
			}
			fileState = nextState
		}
	}
}

type messageRewriteWriter struct {
	target io.Writer
}

func (w *messageRewriteWriter) Write(p []byte) (int, error) {
	rewritten := strings.ReplaceAll(string(p), internalHugoServerMessage, publicEnvyServerMessage)
	rewritten = strings.ReplaceAll(rewritten, hugoFastRenderMessage, envyFastRenderMessage)
	if _, err := io.WriteString(w.target, rewritten); err != nil {
		return 0, err
	}
	return len(p), nil
}

func filterListenFlags(args []string) []string {
	filtered := make([]string, 0, len(args))
	skipNext := false
	for i := 0; i < len(args); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		arg := args[i]
		if arg == "--bind" || arg == "--port" {
			skipNext = true
			continue
		}
		if strings.HasPrefix(arg, "--bind=") || strings.HasPrefix(arg, "--port=") {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
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

func manifestState(path string) (manifestFileState, error) {
	info, err := os.Stat(path)
	if err != nil {
		return manifestFileState{}, fmt.Errorf("checking compose.yaml at %s: %w", path, err)
	}
	return manifestFileState{modTime: info.ModTime(), size: info.Size()}, nil
}

func manifestChanged(path string, previous manifestFileState) (bool, manifestFileState, error) {
	current, err := manifestState(path)
	if err != nil {
		return false, manifestFileState{}, err
	}
	if !current.modTime.Equal(previous.modTime) || current.size != previous.size {
		return true, current, nil
	}
	return false, previous, nil
}

func parseEditorRequestForm(r *http.Request) error {
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return r.ParseMultipartForm(1 << 20)
	}
	return r.ParseForm()
}

func editorAPIHandler(manifestPath string) http.HandlerFunc {
	var writeMu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := parseEditorRequestForm(r); err != nil {
			http.Error(w, "invalid form payload", http.StatusBadRequest)
			return
		}

		varKey := strings.TrimSpace(r.FormValue("key"))
		pagePath := strings.TrimSpace(r.FormValue("page"))
		newValue := r.FormValue("value")
		if varKey == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}

		setSlug, err := extractSetSlugFromPagePath(pagePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		writeMu.Lock()
		err = updateVarDefaultInManifest(manifestPath, setSlug, varKey, newValue)
		writeMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("HX-Trigger", "envyVarSaved")
		w.WriteHeader(http.StatusNoContent)
	}
}

// serviceAPIHandler creates, updates, or deletes services in compose.yml.
// Supported fields via POST: create, name, title, description, link, image, platform.
// DELETE /api/services/{slug}: deletes a service.
func serviceAPIHandler(manifestPath string, afterWrite ...func() error) http.HandlerFunc {
	var writeMu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			serviceSlug := strings.TrimSpace(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/services/"), "/"))
			if serviceSlug == "" || strings.Contains(serviceSlug, "/") {
				http.Error(w, "missing or invalid service slug", http.StatusBadRequest)
				return
			}
			serviceSlug = sanitizeServicePageName(serviceSlug)

			pagePath := strings.TrimSpace(r.URL.Query().Get("page"))
			if pagePath == "" {
				if ref := strings.TrimSpace(r.Referer()); ref != "" {
					if refURL, err := url.Parse(ref); err == nil {
						pagePath = strings.TrimSpace(refURL.Path)
					}
				}
			}
			if extractedSlug, err := extractServiceSlugFromPagePath(pagePath); err != nil {
				http.Error(w, "service deletion is only allowed from service detail pages", http.StatusBadRequest)
				return
			} else if extractedSlug != serviceSlug {
				http.Error(w, "service slug does not match service detail page", http.StatusBadRequest)
				return
			}

			writeMu.Lock()
			err := deleteServiceFromManifest(manifestPath, serviceSlug)
			writeMu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(afterWrite) > 0 && afterWrite[0] != nil {
				if err := afterWrite[0](); err != nil {
					http.Error(w, fmt.Sprintf("failed to refresh generated site: %v", err), http.StatusInternalServerError)
					return
				}
			}

			w.Header().Set("HX-Redirect", serviceIndexPagePath(pagePath))
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := parseEditorRequestForm(r); err != nil {
			http.Error(w, "invalid form payload", http.StatusBadRequest)
			return
		}

		field := strings.TrimSpace(r.FormValue("field"))
		pagePath := strings.TrimSpace(r.FormValue("page"))
		requestKey := strings.TrimSpace(r.FormValue("key"))
		requestValue := r.FormValue("value")

		if field == "create" || (pagePath == "" && requestKey == "" && strings.TrimSpace(requestValue) != "") {
			serviceName := strings.TrimSpace(requestValue)
			if serviceName == "" {
				http.Error(w, "service name cannot be empty", http.StatusBadRequest)
				return
			}

			writeMu.Lock()
			err := addServiceToManifest(manifestPath, serviceName)
			writeMu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(afterWrite) > 0 && afterWrite[0] != nil {
				if err := afterWrite[0](); err != nil {
					http.Error(w, fmt.Sprintf("failed to refresh generated site: %v", err), http.StatusInternalServerError)
					return
				}
			}

			w.Header().Set("HX-Redirect", "/services/"+sanitizeServicePageName(serviceName)+"/")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		serviceSlug, err := editorEntitySlugFromRequest(r, "services")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		writeMu.Lock()
		err = updateServiceFieldInManifest(manifestPath, serviceSlug, field, r.FormValue("value"))
		writeMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(afterWrite) > 0 && afterWrite[0] != nil {
			if err := afterWrite[0](); err != nil {
				http.Error(w, fmt.Sprintf("failed to refresh generated site: %v", err), http.StatusInternalServerError)
				return
			}
		}

		if redirectPath, redirectErr := updatedEntityPagePath(pagePath, "services", sanitizeServicePageName(strings.TrimSpace(r.FormValue("value")))); redirectErr == nil && redirectPath != "" && redirectPath != pagePath && (field == "name" || field == "title") {
			w.Header().Set("HX-Redirect", redirectPath)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func serviceIndexPagePath(pagePath string) string {
	trimmed := strings.TrimSpace(pagePath)
	if trimmed == "" {
		return "/services/"
	}

	const section = "/services/"
	idx := strings.Index(trimmed, section)
	if idx == -1 {
		return "/services/"
	}

	prefix := trimmed[:idx]
	if prefix == "" {
		return "/services/"
	}

	return prefix + section
}

// setAPIHandler creates, updates, or deletes sets in compose.yml.
// Supported fields via POST: create, name, title, description, link.
// DELETE /api/sets/{slug}: deletes a set and all set alias references.
func setAPIHandler(manifestPath string, afterWrite ...func() error) http.HandlerFunc {
	var writeMu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			setSlug := strings.TrimSpace(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/sets/"), "/"))
			if setSlug == "" || strings.Contains(setSlug, "/") {
				http.Error(w, "missing or invalid set slug", http.StatusBadRequest)
				return
			}
			setSlug = sanitizeSetPageName(setSlug)

			pagePath := strings.TrimSpace(r.URL.Query().Get("page"))
			if pagePath == "" {
				if ref := strings.TrimSpace(r.Referer()); ref != "" {
					if refURL, err := url.Parse(ref); err == nil {
						pagePath = strings.TrimSpace(refURL.Path)
					}
				}
			}
			if extractedSlug, err := extractSetSlugFromPagePath(pagePath); err != nil {
				http.Error(w, "set deletion is only allowed from set detail pages", http.StatusBadRequest)
				return
			} else if extractedSlug != setSlug {
				http.Error(w, "set slug does not match set detail page", http.StatusBadRequest)
				return
			}

			writeMu.Lock()
			err := deleteSetFromManifest(manifestPath, setSlug)
			writeMu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(afterWrite) > 0 && afterWrite[0] != nil {
				if err := afterWrite[0](); err != nil {
					http.Error(w, fmt.Sprintf("failed to refresh generated site: %v", err), http.StatusInternalServerError)
					return
				}
			}

			w.Header().Set("HX-Redirect", setIndexPagePath(pagePath))
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := parseEditorRequestForm(r); err != nil {
			http.Error(w, "invalid form payload", http.StatusBadRequest)
			return
		}

		field := strings.TrimSpace(r.FormValue("field"))
		pagePath := strings.TrimSpace(r.FormValue("page"))
		requestKey := strings.TrimSpace(r.FormValue("key"))
		requestValue := r.FormValue("value")

		if field == "create" || (pagePath == "" && requestKey == "" && strings.TrimSpace(requestValue) != "") {
			setName := strings.TrimSpace(requestValue)
			if setName == "" {
				http.Error(w, "set name cannot be empty", http.StatusBadRequest)
				return
			}

			writeMu.Lock()
			err := addSetToManifest(manifestPath, setName)
			writeMu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(afterWrite) > 0 && afterWrite[0] != nil {
				if err := afterWrite[0](); err != nil {
					http.Error(w, fmt.Sprintf("failed to refresh generated site: %v", err), http.StatusInternalServerError)
					return
				}
			}

			w.Header().Set("HX-Redirect", "/sets/"+sanitizeSetPageName(setName)+"/")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		setSlug, err := editorEntitySlugFromRequest(r, "sets")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		writeMu.Lock()
		err = updateSetFieldInManifest(manifestPath, setSlug, field, r.FormValue("value"))
		writeMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(afterWrite) > 0 && afterWrite[0] != nil {
			if err := afterWrite[0](); err != nil {
				http.Error(w, fmt.Sprintf("failed to refresh generated site: %v", err), http.StatusInternalServerError)
				return
			}
		}

		if redirectPath, redirectErr := updatedEntityPagePath(pagePath, "sets", sanitizeSetPageName(strings.TrimSpace(r.FormValue("value")))); redirectErr == nil && redirectPath != "" && redirectPath != pagePath && (field == "name" || field == "title") {
			w.Header().Set("HX-Redirect", redirectPath)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func setIndexPagePath(pagePath string) string {
	trimmed := strings.TrimSpace(pagePath)
	if trimmed == "" {
		return "/sets/"
	}

	const section = "/sets/"
	idx := strings.Index(trimmed, section)
	if idx == -1 {
		return "/sets/"
	}

	prefix := trimmed[:idx]
	if prefix == "" {
		return "/sets/"
	}

	return prefix + section
}

// profileAPIHandler creates, renames, or deletes profile references in compose.yml.
// field=create: adds a new profile (value=<profileName>).
// field=name or field=title: renames an existing profile.
// field=delete: removes a profile from all service profile lists.
func profileAPIHandler(manifestPath string, afterWrite ...func() error) http.HandlerFunc {
	var writeMu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			profileSlug := strings.TrimSpace(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/profiles/"), "/"))
			if profileSlug == "" || strings.Contains(profileSlug, "/") {
				http.Error(w, "missing or invalid profile slug", http.StatusBadRequest)
				return
			}
			profileSlug = sanitizeProfilePageName(profileSlug)

			if profileSlug == "none" {
				http.Error(w, "profile \"none\" is virtual and cannot be renamed or deleted", http.StatusBadRequest)
				return
			}

			pagePath := strings.TrimSpace(r.URL.Query().Get("page"))
			if pagePath == "" {
				if ref := strings.TrimSpace(r.Referer()); ref != "" {
					if refURL, err := url.Parse(ref); err == nil {
						pagePath = strings.TrimSpace(refURL.Path)
					}
				}
			}
			if extractedSlug, err := extractProfileSlugFromPagePath(pagePath); err != nil {
				http.Error(w, "profile deletion is only allowed from profile detail pages", http.StatusBadRequest)
				return
			} else if extractedSlug != profileSlug {
				http.Error(w, "profile slug does not match profile detail page", http.StatusBadRequest)
				return
			}

			writeMu.Lock()
			err := deleteProfileFromManifest(manifestPath, profileSlug)
			writeMu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(afterWrite) > 0 && afterWrite[0] != nil {
				if err := afterWrite[0](); err != nil {
					http.Error(w, fmt.Sprintf("failed to refresh generated site: %v", err), http.StatusInternalServerError)
					return
				}
			}

			w.Header().Set("HX-Redirect", profileIndexPagePath(pagePath))
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := parseEditorRequestForm(r); err != nil {
			http.Error(w, "invalid form payload", http.StatusBadRequest)
			return
		}

		field := strings.TrimSpace(r.FormValue("field"))
		pagePath := strings.TrimSpace(r.FormValue("page"))
		requestKey := strings.TrimSpace(r.FormValue("key"))
		requestValue := r.FormValue("value")

		if field == "create" || (pagePath == "" && requestKey == "" && strings.TrimSpace(requestValue) != "") {
			profileName := strings.TrimSpace(requestValue)
			if profileName == "" {
				http.Error(w, "profile name cannot be empty", http.StatusBadRequest)
				return
			}
			writeMu.Lock()
			err := addProfileToManifest(manifestPath, profileName)
			writeMu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if len(afterWrite) > 0 && afterWrite[0] != nil {
				if err := afterWrite[0](); err != nil {
					http.Error(w, fmt.Sprintf("failed to refresh generated site: %v", err), http.StatusInternalServerError)
					return
				}
			}
			w.Header().Set("HX-Redirect", "/profiles/"+sanitizeProfilePageName(profileName)+"/")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		profileSlug, err := editorEntitySlugFromRequest(r, "profiles")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if profileSlug == "none" {
			http.Error(w, "profile \"none\" is virtual and cannot be renamed or deleted", http.StatusBadRequest)
			return
		}

		writeMu.Lock()
		err = updateProfileFieldInManifest(manifestPath, profileSlug, field, r.FormValue("value"))
		writeMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(afterWrite) > 0 && afterWrite[0] != nil {
			if err := afterWrite[0](); err != nil {
				http.Error(w, fmt.Sprintf("failed to refresh generated site: %v", err), http.StatusInternalServerError)
				return
			}
		}

		if redirectPath, redirectErr := updatedEntityPagePath(pagePath, "profiles", sanitizeProfilePageName(strings.TrimSpace(r.FormValue("value")))); redirectErr == nil && redirectPath != "" && redirectPath != pagePath {
			w.Header().Set("HX-Redirect", redirectPath)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func profileIndexPagePath(pagePath string) string {
	trimmed := strings.TrimSpace(pagePath)
	if trimmed == "" {
		return "/profiles/"
	}

	const section = "/profiles/"
	idx := strings.Index(trimmed, section)
	if idx == -1 {
		return "/profiles/"
	}

	prefix := trimmed[:idx]
	if prefix == "" {
		return "/profiles/"
	}

	return prefix + section
}

func editorEntitySlugFromRequest(r *http.Request, section string) (string, error) {
	if key := sanitizeEditorEntityKey(strings.TrimSpace(r.FormValue("key"))); key != "" {
		return key, nil
	}
	return extractEntitySlugFromPagePath(strings.TrimSpace(r.FormValue("page")), section)
}

func sanitizeEditorEntityKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.Contains(trimmed, "/"):
		return sanitizeSetPageName(trimmed)
	default:
		replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
		return replacer.Replace(trimmed)
	}
}

func extractSetSlugFromPagePath(pagePath string) (string, error) {
	return extractEntitySlugFromPagePath(pagePath, "sets")
}

func extractServiceSlugFromPagePath(pagePath string) (string, error) {
	return extractEntitySlugFromPagePath(pagePath, "services")
}

func extractProfileSlugFromPagePath(pagePath string) (string, error) {
	return extractEntitySlugFromPagePath(pagePath, "profiles")
}

func extractEntitySlugFromPagePath(pagePath, section string) (string, error) {
	trimmed := strings.TrimSpace(pagePath)
	if trimmed == "" {
		return "", fmt.Errorf("missing page path")
	}

	needle := "/" + strings.Trim(section, "/") + "/"
	idx := strings.Index(trimmed, needle)
	if idx == -1 {
		return "", fmt.Errorf("page path %q is not a %s page", pagePath, strings.Trim(section, "/"))
	}

	rest := strings.TrimPrefix(trimmed[idx:], needle)
	slug := strings.TrimSpace(strings.Split(rest, "/")[0])
	if slug == "" {
		return "", fmt.Errorf("could not extract %s slug from page path %q", strings.Trim(section, "/"), pagePath)
	}

	return slug, nil
}

func updatedEntityPagePath(pagePath, section, newSlug string) (string, error) {
	trimmedPagePath := strings.TrimSpace(pagePath)
	trimmedSlug := strings.TrimSpace(newSlug)
	if trimmedPagePath == "" || trimmedSlug == "" {
		return "", nil
	}

	needle := "/" + strings.Trim(section, "/") + "/"
	idx := strings.Index(trimmedPagePath, needle)
	if idx == -1 {
		return "", fmt.Errorf("page path %q is not a %s page", pagePath, strings.Trim(section, "/"))
	}

	prefix := trimmedPagePath[:idx+len(needle)]
	rest := trimmedPagePath[idx+len(needle):]
	segments := strings.Split(rest, "/")
	if len(segments) == 0 || strings.TrimSpace(segments[0]) == "" {
		return "", fmt.Errorf("could not extract %s slug from page path %q", strings.Trim(section, "/"), pagePath)
	}
	segments[0] = trimmedSlug
	updatedPath := prefix + strings.Join(segments, "/")
	if strings.HasSuffix(trimmedPagePath, "/") && !strings.HasSuffix(updatedPath, "/") {
		updatedPath += "/"
	}

	return updatedPath, nil
}

func updateVarDefaultInManifest(manifestPath, setSlug, varKey, newValue string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applyVarUpdateToManifestNode(document, setSlug, varKey, newValue)
	})
}

func updateServiceFieldInManifest(manifestPath, serviceSlug, field, newValue string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applyServiceFieldUpdateToManifestNode(document, serviceSlug, field, newValue)
	})
}

func addServiceToManifest(manifestPath, serviceName string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applyAddServiceToManifestNode(document, serviceName)
	})
}

func deleteServiceFromManifest(manifestPath, serviceSlug string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applyServiceDeleteToManifestNode(document, serviceSlug)
	})
}

func updateSetFieldInManifest(manifestPath, setSlug, field, newValue string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applySetFieldUpdateToManifestNode(document, setSlug, field, newValue)
	})
}

func deleteSetFromManifest(manifestPath, setSlug string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applySetDeleteToManifestNode(document, setSlug)
	})
}

func addSetToManifest(manifestPath, setName string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applyAddSetToManifestNode(document, setName)
	})
}

func updateProfileFieldInManifest(manifestPath, profileSlug, field, newValue string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applyProfileFieldUpdateToManifestNode(document, profileSlug, field, newValue)
	})
}

func deleteProfileFromManifest(manifestPath, profileSlug string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applyProfileDeleteToManifestNode(document, profileSlug)
	})
}

func updateManifest(manifestPath string, apply func(document *yaml.Node) error) error {
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading compose manifest: %w", err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return fmt.Errorf("parsing compose manifest: %w", err)
	}

	if err := apply(&document); err != nil {
		return err
	}

	if len(document.Content) == 0 {
		return fmt.Errorf("compose manifest is empty")
	}

	updated, err := yaml.Marshal(document.Content[0])
	if err != nil {
		return fmt.Errorf("serializing compose manifest: %w", err)
	}

	if err := validateUpdatedManifest(manifestPath, updated); err != nil {
		return err
	}

	if err := writeFileAtomically(manifestPath, updated); err != nil {
		return fmt.Errorf("writing compose manifest: %w", err)
	}

	return nil
}

func applyServiceFieldUpdateToManifestNode(document *yaml.Node, serviceSlug, field, newValue string) error {
	root, err := manifestRootNode(document)
	if err != nil {
		return err
	}

	servicesNode := mappingValueNode(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return fmt.Errorf("compose manifest has no services mapping")
	}

	for i := 0; i < len(servicesNode.Content); i += 2 {
		keyNode := servicesNode.Content[i]
		valueNode := servicesNode.Content[i+1]
		if sanitizeServicePageName(keyNode.Value) != serviceSlug {
			continue
		}

		switch strings.TrimSpace(field) {
		case "name", "title":
			trimmedValue := strings.TrimSpace(newValue)
			if trimmedValue == "" {
				return fmt.Errorf("service name cannot be empty")
			}
			if err := ensureUniqueServiceSlug(servicesNode, i, trimmedValue); err != nil {
				return err
			}
			keyNode.Value = trimmedValue
		case "description":
			description, link := extractEntryMetadataFromComments(servicesNode, i)
			setEntryLeadingComment(servicesNode, i, newValue, link)
			_ = description
		case "link":
			description, _ := extractEntryMetadataFromComments(servicesNode, i)
			setEntryLeadingComment(servicesNode, i, description, newValue)
		case "image":
			upsertMappingScalarField(valueNode, "image", newValue)
		case "platform":
			upsertMappingScalarField(valueNode, "platform", newValue)
		default:
			return fmt.Errorf("unsupported service field %q", field)
		}
		return nil
	}

	return fmt.Errorf("service %q not found in compose manifest", serviceSlug)
}

func applyAddServiceToManifestNode(document *yaml.Node, serviceName string) error {
	trimmed := strings.TrimSpace(serviceName)
	if trimmed == "" {
		return fmt.Errorf("service name cannot be empty")
	}

	root, err := manifestRootNode(document)
	if err != nil {
		return err
	}

	servicesNode := mappingValueNode(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return fmt.Errorf("compose manifest has no services mapping")
	}

	newSlug := sanitizeServicePageName(trimmed)
	for i := 0; i < len(servicesNode.Content); i += 2 {
		if sanitizeServicePageName(servicesNode.Content[i].Value) == newSlug {
			return fmt.Errorf("service %q already exists", trimmed)
		}
	}

	serviceNode := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "image"},
		newScalarNode("busybox:latest"),
	}}
	servicesNode.Content = append(servicesNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: trimmed},
		serviceNode,
	)

	return nil
}

func applyServiceDeleteToManifestNode(document *yaml.Node, serviceSlug string) error {
	root, err := manifestRootNode(document)
	if err != nil {
		return err
	}

	servicesNode := mappingValueNode(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return fmt.Errorf("compose manifest has no services mapping")
	}

	for i := 0; i < len(servicesNode.Content); i += 2 {
		if sanitizeServicePageName(servicesNode.Content[i].Value) != serviceSlug {
			continue
		}
		servicesNode.Content = append(servicesNode.Content[:i], servicesNode.Content[i+2:]...)
		return nil
	}

	return fmt.Errorf("service %q not found in compose manifest", serviceSlug)
}

func ensureUniqueServiceSlug(servicesNode *yaml.Node, currentKeyIndex int, newValue string) error {
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return fmt.Errorf("compose manifest has no services mapping")
	}
	newSlug := sanitizeServicePageName(newValue)
	for i := 0; i < len(servicesNode.Content); i += 2 {
		if i == currentKeyIndex {
			continue
		}
		if sanitizeServicePageName(servicesNode.Content[i].Value) == newSlug {
			return fmt.Errorf("service name %q conflicts with existing service %q", newValue, servicesNode.Content[i].Value)
		}
	}
	return nil
}

func applySetFieldUpdateToManifestNode(document *yaml.Node, setSlug, field, newValue string) error {
	root, err := manifestRootNode(document)
	if err != nil {
		return err
	}

	for i := 0; i < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valueNode := root.Content[i+1]

		if strings.HasPrefix(keyNode.Value, "x-set-") {
			setKey := strings.TrimPrefix(keyNode.Value, "x-set-")
			if sanitizeSetPageName(setKey) != setSlug {
				continue
			}
			description, link := extractEntryMetadataFromComments(root, i)
			switch strings.TrimSpace(field) {
			case "name", "title":
				trimmedValue := strings.TrimSpace(newValue)
				if trimmedValue == "" {
					return fmt.Errorf("set name cannot be empty")
				}
				if err := ensureUniqueSetSlug(root, i, trimmedValue); err != nil {
					return err
				}
				oldAnchor := valueNode.Anchor
				keyNode.Value = "x-set-" + trimmedValue
				if oldAnchor != "" {
					valueNode.Anchor = trimmedValue
					updateAliasReferences(document, valueNode, oldAnchor, trimmedValue)
				}
			case "description":
				setEntryLeadingComment(root, i, newValue, link)
			case "link":
				setEntryLeadingComment(root, i, description, newValue)
			default:
				return applySetFieldUpdate(valueNode, field, newValue)
			}
			return nil
		}

		if keyNode.Value == "sets" && valueNode.Kind == yaml.MappingNode {
			for j := 0; j < len(valueNode.Content); j += 2 {
				setKeyNode := valueNode.Content[j]
				setValueNode := valueNode.Content[j+1]
				if sanitizeSetPageName(setKeyNode.Value) != setSlug {
					continue
				}
				if strings.TrimSpace(field) == "name" || strings.TrimSpace(field) == "title" {
					trimmedValue := strings.TrimSpace(newValue)
					if trimmedValue == "" {
						return fmt.Errorf("set name cannot be empty")
					}
					setKeyNode.Value = trimmedValue
					return nil
				}
				return applySetFieldUpdate(setValueNode, field, newValue)
			}
		}
	}

	return fmt.Errorf("set %q not found in compose manifest", setSlug)
}

func ensureUniqueSetSlug(root *yaml.Node, currentKeyIndex int, newValue string) error {
	newSlug := sanitizeSetPageName(newValue)
	for i := 0; i < len(root.Content); i += 2 {
		if i == currentKeyIndex {
			continue
		}
		keyNode := root.Content[i]
		if strings.HasPrefix(keyNode.Value, "x-set-") {
			existingKey := strings.TrimPrefix(keyNode.Value, "x-set-")
			if sanitizeSetPageName(existingKey) == newSlug {
				return fmt.Errorf("set name %q conflicts with existing set %q", newValue, existingKey)
			}
		}
	}
	return nil
}

// updateAliasReferences walks the YAML node tree and renames all alias nodes
// whose Alias pointer matches anchoredNode, updating their Value to newAnchor.
func updateAliasReferences(n *yaml.Node, anchoredNode *yaml.Node, oldAnchor, newAnchor string) {
	if n == nil {
		return
	}
	for _, child := range n.Content {
		if child.Kind == yaml.AliasNode && child.Alias == anchoredNode {
			child.Value = newAnchor
		}
		updateAliasReferences(child, anchoredNode, oldAnchor, newAnchor)
	}
}

func applySetDeleteToManifestNode(document *yaml.Node, setSlug string) error {
	root, err := manifestRootNode(document)
	if err != nil {
		return err
	}

	found := false
	for i := 0; i < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		if !strings.HasPrefix(keyNode.Value, "x-set-") {
			continue
		}

		setKey := strings.TrimPrefix(keyNode.Value, "x-set-")
		if sanitizeSetPageName(setKey) != setSlug {
			continue
		}

		setNode := root.Content[i+1]
		anchorName := strings.TrimSpace(setNode.Anchor)

		root.Content = append(root.Content[:i], root.Content[i+2:]...)
		if anchorName != "" {
			removeSetAliasReferences(document, anchorName)
		}
		found = true
		break
	}

	if found {
		return nil
	}

	setsNode := mappingValueNode(root, "sets")
	if setsNode != nil && setsNode.Kind == yaml.MappingNode {
		for i := 0; i < len(setsNode.Content); i += 2 {
			if sanitizeSetPageName(setsNode.Content[i].Value) != setSlug {
				continue
			}
			setsNode.Content = append(setsNode.Content[:i], setsNode.Content[i+2:]...)
			return nil
		}
	}

	return fmt.Errorf("set %q not found in compose manifest", setSlug)
}

func removeSetAliasReferences(node *yaml.Node, anchorName string) {
	if node == nil {
		return
	}

	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); {
			key := node.Content[i]
			value := node.Content[i+1]

			if aliasMatchesAnchor(value, anchorName) {
				node.Content = append(node.Content[:i], node.Content[i+2:]...)
				continue
			}

			removeSetAliasReferences(value, anchorName)
			if key.Value == "<<" && value.Kind == yaml.SequenceNode && len(value.Content) == 0 {
				node.Content = append(node.Content[:i], node.Content[i+2:]...)
				continue
			}

			i += 2
		}
	case yaml.SequenceNode:
		filtered := node.Content[:0]
		for _, child := range node.Content {
			if aliasMatchesAnchor(child, anchorName) {
				continue
			}
			removeSetAliasReferences(child, anchorName)
			filtered = append(filtered, child)
		}
		node.Content = filtered
	default:
		for _, child := range node.Content {
			removeSetAliasReferences(child, anchorName)
		}
	}
}

func aliasMatchesAnchor(node *yaml.Node, anchorName string) bool {
	if node == nil || node.Kind != yaml.AliasNode {
		return false
	}
	if strings.TrimSpace(node.Value) == anchorName {
		return true
	}
	return node.Alias != nil && strings.TrimSpace(node.Alias.Anchor) == anchorName
}

func applyAddSetToManifestNode(document *yaml.Node, setName string) error {
	trimmed := strings.TrimSpace(setName)
	if trimmed == "" {
		return fmt.Errorf("set name cannot be empty")
	}

	root, err := manifestRootNode(document)
	if err != nil {
		return err
	}

	newSlug := sanitizeSetPageName(trimmed)
	for i := 0; i < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		if strings.HasPrefix(keyNode.Value, "x-set-") {
			existingKey := strings.TrimPrefix(keyNode.Value, "x-set-")
			if sanitizeSetPageName(existingKey) == newSlug {
				return fmt.Errorf("set %q already exists", trimmed)
			}
		}
		if keyNode.Value == "sets" {
			setsNode := root.Content[i+1]
			if setsNode.Kind == yaml.MappingNode {
				for j := 0; j < len(setsNode.Content); j += 2 {
					if sanitizeSetPageName(setsNode.Content[j].Value) == newSlug {
						return fmt.Errorf("set %q already exists", trimmed)
					}
				}
			}
		}
	}

	insertAt := len(root.Content)
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "services" {
			insertAt = i
			break
		}
	}

	newKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "x-set-" + trimmed}
	newValue := &yaml.Node{Kind: yaml.MappingNode, Anchor: trimmed, Content: []*yaml.Node{}}

	root.Content = append(root.Content, nil, nil)
	copy(root.Content[insertAt+2:], root.Content[insertAt:])
	root.Content[insertAt] = newKey
	root.Content[insertAt+1] = newValue

	return nil
}

func applySetFieldUpdate(setNode *yaml.Node, field, newValue string) error {
	if setNode == nil || setNode.Kind != yaml.MappingNode {
		return fmt.Errorf("set node is not a mapping")
	}

	switch strings.TrimSpace(field) {
	case "description":
		upsertMappingScalarField(setNode, "description", newValue)
	case "link":
		upsertMappingScalarField(setNode, "link", newValue)
	default:
		return fmt.Errorf("unsupported set field %q", field)
	}

	return nil
}

func setEntryLeadingComment(root *yaml.Node, keyIndex int, description, link string) {
	if root == nil || keyIndex < 0 || keyIndex+1 >= len(root.Content) {
		return
	}
	keyNode := root.Content[keyIndex]
	valueNode := root.Content[keyIndex+1]
	comment := buildEntryLeadingComment(description, link)
	keyNode.HeadComment = comment
	keyNode.LineComment = ""
	valueNode.HeadComment = ""
	valueNode.LineComment = ""
	if keyIndex >= 2 {
		root.Content[keyIndex-1].FootComment = ""
	}
}

func buildEntryLeadingComment(description, link string) string {
	parts := make([]string, 0, 2)
	if trimmedDescription := strings.TrimSpace(description); trimmedDescription != "" {
		parts = append(parts, trimmedDescription)
	}
	if trimmedLink := strings.TrimSpace(link); trimmedLink != "" {
		parts = append(parts, "Link: "+trimmedLink)
	}
	return strings.Join(parts, "\n")
}

func extractEntryMetadataFromComments(root *yaml.Node, keyIndex int) (string, string) {
	if root == nil || keyIndex < 0 || keyIndex+1 >= len(root.Content) {
		return "", ""
	}

	keyNode := root.Content[keyIndex]
	valueNode := root.Content[keyIndex+1]
	comments := []string{
		keyNode.HeadComment,
		keyNode.LineComment,
		valueNode.HeadComment,
		valueNode.LineComment,
	}
	if keyIndex >= 2 {
		comments = append([]string{root.Content[keyIndex-1].FootComment}, comments...)
	}

	var descriptionParts []string
	link := ""
	for _, raw := range comments {
		for _, line := range strings.Split(raw, "\n") {
			trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if matches := editorPrefixedLinkPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
				if link == "" {
					link = strings.TrimSpace(matches[1])
				}
				continue
			}
			if editorPlainLinkPattern.MatchString(trimmed) {
				if link == "" {
					link = trimmed
				}
				continue
			}
			descriptionParts = append(descriptionParts, trimmed)
		}
	}

	return strings.TrimSpace(strings.Join(descriptionParts, " ")), link
}

func applyProfileFieldUpdateToManifestNode(document *yaml.Node, profileSlug, field, newValue string) error {
	if strings.TrimSpace(field) != "name" && strings.TrimSpace(field) != "title" {
		return fmt.Errorf("unsupported profile field %q", field)
	}
	trimmedValue := strings.TrimSpace(newValue)
	if trimmedValue == "" {
		return fmt.Errorf("profile name cannot be empty")
	}

	root, err := manifestRootNode(document)
	if err != nil {
		return err
	}

	servicesNode := mappingValueNode(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return fmt.Errorf("compose manifest has no services mapping")
	}

	found := false
	for i := 0; i < len(servicesNode.Content); i += 2 {
		serviceNode := servicesNode.Content[i+1]
		profilesNode := mappingValueNode(serviceNode, "profiles")
		if profilesNode == nil {
			continue
		}
		updated, err := replaceProfileReferences(profilesNode, profileSlug, trimmedValue)
		if err != nil {
			return err
		}
		if updated {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("profile %q not found in compose manifest", profileSlug)
	}

	return nil
}

func addProfileToManifest(manifestPath, profileName string) error {
	return updateManifest(manifestPath, func(document *yaml.Node) error {
		return applyAddProfileToManifestNode(document, profileName)
	})
}

// applyAddProfileToManifestNode adds a new profile name to compose services.
// It appends the profile to the first service that already has profiles; if no
// such service exists, it adds the profile to the first service in the file.
func applyAddProfileToManifestNode(document *yaml.Node, profileName string) error {
	trimmed := strings.TrimSpace(profileName)
	if trimmed == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if sanitizeProfilePageName(trimmed) == "none" {
		return fmt.Errorf("profile %q is reserved", trimmed)
	}

	root, err := manifestRootNode(document)
	if err != nil {
		return err
	}

	servicesNode := mappingValueNode(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode || len(servicesNode.Content) < 2 {
		return fmt.Errorf("compose manifest has no services")
	}

	// Reject if profile already exists in any service.
	newSlug := sanitizeProfilePageName(trimmed)
	for i := 0; i < len(servicesNode.Content); i += 2 {
		profilesNode := mappingValueNode(servicesNode.Content[i+1], "profiles")
		if profilesNode == nil {
			continue
		}
		switch profilesNode.Kind {
		case yaml.SequenceNode:
			for _, item := range profilesNode.Content {
				if item.Kind == yaml.ScalarNode && sanitizeProfilePageName(strings.TrimSpace(item.Value)) == newSlug {
					return fmt.Errorf("profile %q already exists", trimmed)
				}
			}
		case yaml.ScalarNode:
			if sanitizeProfilePageName(strings.TrimSpace(profilesNode.Value)) == newSlug {
				return fmt.Errorf("profile %q already exists", trimmed)
			}
		}
	}

	// Find the first service that already has a profiles list, falling back to
	// the first service if none do.
	targetIdx := -1
	for i := 0; i < len(servicesNode.Content); i += 2 {
		if mappingValueNode(servicesNode.Content[i+1], "profiles") != nil {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		targetIdx = 0
	}

	serviceNode := servicesNode.Content[targetIdx+1]
	profilesNode := mappingValueNode(serviceNode, "profiles")
	if profilesNode == nil {
		serviceNode.Content = append(serviceNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "profiles"},
			&yaml.Node{
				Kind:    yaml.SequenceNode,
				Content: []*yaml.Node{newScalarNode(trimmed)},
			},
		)
	} else {
		switch profilesNode.Kind {
		case yaml.SequenceNode:
			profilesNode.Content = append(profilesNode.Content, newScalarNode(trimmed))
		case yaml.ScalarNode:
			existing := profilesNode.Value
			profilesNode.Kind = yaml.SequenceNode
			profilesNode.Value = ""
			profilesNode.Tag = ""
			profilesNode.Content = []*yaml.Node{newScalarNode(existing), newScalarNode(trimmed)}
		}
	}

	return nil
}

func manifestRootNode(document *yaml.Node) (*yaml.Node, error) {
	if document == nil || len(document.Content) == 0 {
		return nil, fmt.Errorf("compose manifest is empty")
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("compose manifest root must be a mapping")
	}
	return root, nil
}

func mappingValueNode(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func upsertMappingScalarField(mapping *yaml.Node, key, value string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	trimmed := strings.TrimSpace(value)
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		if trimmed == "" {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
		setScalarNodeValue(mapping.Content[i+1], value)
		return
	}
	if trimmed == "" {
		return
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		newScalarNode(value),
	)
}

func newScalarNode(value string) *yaml.Node {
	node := &yaml.Node{}
	setScalarNodeValue(node, value)
	return node
}

func setScalarNodeValue(node *yaml.Node, value string) {
	if node == nil {
		return
	}
	node.Kind = yaml.ScalarNode
	node.Tag = "!!str"
	node.Value = value
	if strings.Contains(value, "\n") {
		node.Style = yaml.LiteralStyle
		return
	}
	node.Style = 0
}

func replaceProfileReferences(node *yaml.Node, profileSlug, newValue string) (bool, error) {
	updated := false
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				continue
			}
			if sanitizeProfilePageName(item.Value) != profileSlug {
				continue
			}
			setScalarNodeValue(item, newValue)
			updated = true
		}
	case yaml.ScalarNode:
		if sanitizeProfilePageName(node.Value) == profileSlug {
			setScalarNodeValue(node, newValue)
			updated = true
		}
	default:
		return false, fmt.Errorf("profiles node must be scalar or sequence")
	}
	return updated, nil
}

func applyProfileDeleteToManifestNode(document *yaml.Node, profileSlug string) error {
	root, err := manifestRootNode(document)
	if err != nil {
		return err
	}

	servicesNode := mappingValueNode(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return fmt.Errorf("compose manifest has no services mapping")
	}

	found := false
	for i := 0; i < len(servicesNode.Content); i += 2 {
		serviceNode := servicesNode.Content[i+1]
		if serviceNode == nil || serviceNode.Kind != yaml.MappingNode {
			continue
		}

		profilesIndex := -1
		for j := 0; j < len(serviceNode.Content); j += 2 {
			if serviceNode.Content[j].Value == "profiles" {
				profilesIndex = j
				break
			}
		}
		if profilesIndex == -1 {
			continue
		}

		profilesNode := serviceNode.Content[profilesIndex+1]
		switch profilesNode.Kind {
		case yaml.SequenceNode:
			filtered := profilesNode.Content[:0]
			removedFromService := false
			for _, item := range profilesNode.Content {
				if item.Kind == yaml.ScalarNode && sanitizeProfilePageName(strings.TrimSpace(item.Value)) == profileSlug {
					removedFromService = true
					continue
				}
				filtered = append(filtered, item)
			}
			if !removedFromService {
				continue
			}
			found = true
			profilesNode.Content = filtered
			if len(profilesNode.Content) == 0 {
				serviceNode.Content = append(serviceNode.Content[:profilesIndex], serviceNode.Content[profilesIndex+2:]...)
			}
		case yaml.ScalarNode:
			if sanitizeProfilePageName(strings.TrimSpace(profilesNode.Value)) != profileSlug {
				continue
			}
			found = true
			serviceNode.Content = append(serviceNode.Content[:profilesIndex], serviceNode.Content[profilesIndex+2:]...)
		default:
			return fmt.Errorf("profiles node must be scalar or sequence")
		}
	}

	if !found {
		return fmt.Errorf("profile %q not found in compose manifest", profileSlug)
	}

	return nil
}

func validateUpdatedManifest(manifestPath string, content []byte) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(manifestPath), "compose-update-*.yml")
	if err != nil {
		return fmt.Errorf("creating validation file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing validation file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing validation file: %w", err)
	}

	if err := validateComposeFile(tmpPath); err != nil {
		return fmt.Errorf("updated compose.yml is invalid: %w", err)
	}

	return nil
}

func writeFileAtomically(path string, content []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(path), "compose-write-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(info.Mode()); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

func applyVarUpdateToManifestNode(document *yaml.Node, setSlug, varKey, newValue string) error {
	if document == nil || len(document.Content) == 0 {
		return fmt.Errorf("compose manifest is empty")
	}

	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("compose manifest root must be a mapping")
	}

	for i := 0; i < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valueNode := root.Content[i+1]

		if strings.HasPrefix(keyNode.Value, "x-set-") {
			setKey := strings.TrimPrefix(keyNode.Value, "x-set-")
			if sanitizeSetPageName(setKey) != setSlug {
				continue
			}
			updated, err := updateVarInSetNode(valueNode, varKey, newValue)
			if err != nil {
				return err
			}
			if !updated {
				return fmt.Errorf("variable %q not found in set %q", varKey, setKey)
			}
			return nil
		}

		if keyNode.Value == "sets" && valueNode.Kind == yaml.MappingNode {
			for j := 0; j < len(valueNode.Content); j += 2 {
				setKeyNode := valueNode.Content[j]
				setValueNode := valueNode.Content[j+1]
				if sanitizeSetPageName(setKeyNode.Value) != setSlug {
					continue
				}
				updated, err := updateVarInSetNode(setValueNode, varKey, newValue)
				if err != nil {
					return err
				}
				if !updated {
					return fmt.Errorf("variable %q not found in set %q", varKey, setKeyNode.Value)
				}
				return nil
			}
		}
	}

	return fmt.Errorf("set %q not found in compose manifest", setSlug)
}

func updateVarInSetNode(setNode *yaml.Node, varKey, newValue string) (bool, error) {
	if setNode == nil || setNode.Kind != yaml.MappingNode {
		return false, fmt.Errorf("set node is not a mapping")
	}

	for i := 0; i < len(setNode.Content); i += 2 {
		keyNode := setNode.Content[i]
		valueNode := setNode.Content[i+1]

		if keyNode.Value == "vars" {
			updated, err := updateVarInVarsNode(valueNode, varKey, newValue)
			return updated, err
		}

		if keyNode.Value == "description" || keyNode.Value == "link" {
			continue
		}

		if keyNode.Value == varKey {
			updateVarValueNode(varKey, valueNode, newValue)
			return true, nil
		}
	}

	return false, nil
}

func updateVarInVarsNode(varsNode *yaml.Node, varKey, newValue string) (bool, error) {
	if varsNode == nil {
		return false, nil
	}

	switch varsNode.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(varsNode.Content); i += 2 {
			if varsNode.Content[i].Value != varKey {
				continue
			}
			updateVarValueNode(varKey, varsNode.Content[i+1], newValue)
			return true, nil
		}
	case yaml.SequenceNode:
		for _, item := range varsNode.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			for i := 0; i < len(item.Content); i += 2 {
				if item.Content[i].Value != "key" || item.Content[i+1].Value != varKey {
					continue
				}
				for j := 0; j < len(item.Content); j += 2 {
					if item.Content[j].Value == "default" {
						updateVarValueNode(varKey, item.Content[j+1], newValue)
						return true, nil
					}
				}
				item.Content = append(item.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Value: "default"},
					&yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: newValue},
				)
				return true, nil
			}
		}
	default:
		return false, fmt.Errorf("vars node must be mapping or sequence")
	}

	return false, nil
}

func updateVarValueNode(varKey string, valueNode *yaml.Node, newValue string) {
	if valueNode == nil {
		return
	}

	if valueNode.Kind == yaml.MappingNode {
		for i := 0; i < len(valueNode.Content); i += 2 {
			if valueNode.Content[i].Value != "default" {
				continue
			}
			updateVarValueNode(varKey, valueNode.Content[i+1], newValue)
			return
		}
		valueNode.Content = append(valueNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "default"},
			&yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: newValue},
		)
		return
	}

	if valueNode.Kind != yaml.ScalarNode {
		return
	}

	updated := updatedScalarForVar(varKey, valueNode.Value, newValue)
	valueNode.Value = updated
	if valueNode.Style == 0 {
		valueNode.Style = yaml.DoubleQuotedStyle
	}
}

func updatedScalarForVar(varKey, currentValue, newValue string) string {
	trimmed := strings.TrimSpace(currentValue)
	matches := composeInterpolationPatternLocal.FindStringSubmatch(trimmed)
	if len(matches) > 0 && matches[1] == varKey {
		switch matches[2] {
		case ":-", "-":
			return fmt.Sprintf("${%s%s%s}", varKey, matches[2], newValue)
		case "", ":?", "?":
			return fmt.Sprintf("${%s:-%s}", varKey, newValue)
		}
	}
	return newValue
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

func prepareBuildAssets(path string, refreshPersistentContent bool, editorAPIURL string) (string, error) {
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

	if err := syncBuildAssets(path, siteDir, refreshPersistentContent, editorAPIURL); err != nil {
		os.RemoveAll(siteDir)
		return "", err
	}

	return siteDir, nil
}

func syncBuildAssets(path, siteDir string, refreshPersistentContent bool, editorAPIURL string) error {
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

	if err := writeTempHugoConfigFromManifest(m, siteDir, repoURL, editorAPIURL); err != nil {
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

func writeTempHugoConfigFromManifest(m *compose.Project, siteDir string, repoURL string, editorAPIURL string) error {
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
	if strings.TrimSpace(editorAPIURL) != "" {
		params["envyEditor"] = map[string]interface{}{
			"enabled": true,
			"apiURL":  strings.TrimSpace(editorAPIURL),
		}
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

func hugoConfigValue(metaValue string, lookup map[string]*string, key string) string {
	if trimmed := strings.TrimSpace(metaValue); trimmed != "" {
		return trimmed
	}
	if v, ok := lookup[key]; ok {
		return strings.TrimSpace(compose.VarString(v))
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

func buildVarLookup(m *compose.Project) map[string]*string {
	lookup := make(map[string]*string)
	for key, value := range m.AllVars() {
		lookup[key] = value
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
		pagePath := filepath.Join(setsDir, sanitizeSetPageName(set.Key())+".md")
		if err := writeGeneratedFile(contentDir, pagePath, generateSetMarkdown(m, set, defaultLanguage)); err != nil {
			return "", err
		}
		for _, language := range additionalLanguages {
			localizedPagePath := filepath.Join(setsDir, sanitizeSetPageName(set.Key())+"."+language+".md")
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
		services := servicesForSet(m, set.Key())
		body.WriteString(renderSetOverviewCard(set, services, language))
	}
	body.WriteString(renderAddSetForm())
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
	body.WriteString(renderAddServiceForm())
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
	body.WriteString(renderAddProfileForm())
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
		"title":       set.Key(),
		"description": strings.TrimSpace(set.Description()),
		"weight":      setWeight(m, set.Key()),
		"hideTitle":   true,
		"toc":         false,
	}))

	// Add set card at the top
	services := servicesForSet(m, set.Key())
	body.WriteString(renderCardsOpen(1))
	body.WriteString(renderSetCard(set, services, language))
	body.WriteString(renderCardsClose())
	body.WriteString("\n")

	if len(set.Vars()) == 0 {
		body.WriteString(renderInfoCallout(generatedPageString(language, "setNoVariables")))
		return body.String()
	}
	for _, key := range compose.SortedVarKeys(set.Vars()) {
		variable := hugoVar{Key: key, Value: set.Vars()[key]}
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
		rawCommand := commandLiteral(service.Command)
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

// commandLiteral formats a Docker Compose list-form command value.
// Example: [ "valkey-server", "--loglevel", "warning" ]
func commandLiteral(args []string) string {
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
		if set.Key() == setKey {
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

type hugoVar struct {
	Key   string
	Value *string
}

func varsForServiceSorted(m *compose.Project, service compose.Service) []hugoVar {
	byKey := make(map[string]*string)
	for _, setKey := range service.Sets {
		set, ok := m.Sets[setKey]
		if !ok {
			continue
		}
		for key, value := range set.Vars() {
			byKey[key] = value
		}
	}

	vars := make([]hugoVar, 0, len(byKey))
	for key, value := range byKey {
		vars = append(vars, hugoVar{Key: key, Value: value})
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

func renderAddProfileForm() string {
	return "{{< add-profile >}}\n"
}

func renderAddSetForm() string {
	return "{{< add-set >}}\n"
}

func renderAddServiceForm() string {
	return "{{< add-service >}}\n"
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
		escapeShortcodeValue(set.Key()),
	))

	// Add description.
	if strings.TrimSpace(set.Description()) != "" {
		sb.WriteString(fmt.Sprintf(" description=`%s`", escapeShortcodeRawValue(strings.TrimSpace(set.Description()))))
	}

	// Add documentation link.
	if strings.TrimSpace(set.Link()) != "" {
		_, linkTarget := normalizeSetDocLink(set.Link())
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
		escapeShortcodeValue(set.Key()),
		escapeShortcodeValue("/sets/"+sanitizeSetPageName(set.Key())+"/"),
	))

	description := strings.TrimSpace(defaultString(set.Description(), generatedPageString(language, "setDescriptionFallback")))
	if description != "" {
		sb.WriteString(fmt.Sprintf(" description=`%s`", escapeShortcodeRawValue(description)))
	}

	if strings.TrimSpace(set.Link()) != "" {
		_, linkTarget := normalizeSetDocLink(set.Link())
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

func renderVarCard(variable hugoVar, description, class string, showQuestionPrefixAsRequired bool, language string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("{{< card link=\"\" title=\"%s\" cardType=\"var\"",
		escapeShortcodeValue(variable.Key),
	))
	if strings.TrimSpace(description) != "" {
		sb.WriteString(fmt.Sprintf(" description=`%s`", escapeShortcodeRawValue(description)))
	}
	defaultValue := strings.TrimSpace(compose.VarString(variable.Value))
	hasQuestionPrefix := showQuestionPrefixAsRequired && strings.HasPrefix(defaultValue, "?")
	if hasQuestionPrefix {
		defaultValue = strings.TrimPrefix(defaultValue, "?")
	}
	if defaultValue != "" {
		sb.WriteString(fmt.Sprintf(" var=\"%s\"", escapeShortcodeValue(defaultValue)))
	}
	if hasQuestionPrefix {
		sb.WriteString(fmt.Sprintf(" tagBottom=\"%s\" tagBottomColor=\"red\"", escapeShortcodeValue(generatedPageString(language, "required"))))
	}
	if class != "" {
		sb.WriteString(fmt.Sprintf(" class=\"%s\"", escapeShortcodeValue(class)))
	}
	sb.WriteString(" >}}\n")
	return sb.String()
}

func variableCardTag(variable hugoVar, language string) (string, string, string) {
	_ = variable
	_ = language
	return "", "", ""
}

func variableCardClass(variable hugoVar) string {
	_ = variable
	return ""
}

func variableCardSubtitle(variable hugoVar, language string) string {
	parts := make([]string, 0, 5)
	_ = language

	defaultValue := strings.TrimSpace(compose.VarString(variable.Value))
	if len(parts) == 0 {
		if defaultValue != "" {
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
