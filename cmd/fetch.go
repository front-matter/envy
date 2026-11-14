package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/fatih/color"
	"github.com/front-matter/envy/compose"
	"github.com/front-matter/envy/envfile"
	"github.com/spf13/cobra"
)

var (
	fetchFilePath string

	// repoComposeFilenames are the compose files looked up in a repository root,
	// in order of preference.
	repoComposeFilenames = []string{"compose.yml", "compose.yaml"}

	// repoHosts are the code hosts whose repository URLs are recognised.
	repoHosts = []string{"github.com", "gitlab.com", "codeberg.org"}
)

var fetchCmd = &cobra.Command{
	Use:   "fetch <url>",
	Short: "Fetch compose.yml from a code repository or URL",
	Long: `Fetch a compose file from a code repository or URL and write it locally.

Source: The <url> argument can be
- a repository URL on github.com, gitlab.com or codeberg.org. The scheme is optional,
  like a Go module path. Fetches compose.yml, or else compose.yaml, from the
  repository root on the default branch.
- an http(s) URL ending in .yml/.yaml, which is fetched as a compose file.

The compose file is validated and written unchanged, preserving comments. To convert
.env files into compose.yml, use "envy import".

File paths: The --file flag can be either a folder or a file path ending in .yaml/.yml.
- Folder: creates the folder if needed and writes to folder/compose.yml
- File: .yaml/.yml file path, creates parent directories as needed

Examples:
  envy fetch https://github.com/front-matter/envy
  envy fetch github.com/front-matter/envy
  envy fetch gitlab.com/group/subgroup/repo
  envy fetch codeberg.org/org/repo
  envy fetch https://example.org/stack/compose.yml
  envy fetch github.com/front-matter/envy --file config/`,
	Args: cobra.ExactArgs(1),
	// fetch writes no .env file, so it does not need the .gitignore check of the
	// root command; it often runs in an empty directory to bootstrap a project.
	// A command's own persistent pre-run replaces the root's.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		source, err := fetchSourceURL(args[0])
		if err != nil {
			return err
		}

		path, err := resolvePath(fetchFilePath)
		if err != nil {
			return err
		}

		// Do not overwrite existing files.
		exists, err := FileExists(path)
		if err != nil {
			return err
		}
		if exists {
			color.Yellow("Warning: %s already exists; not writing file.", path)
			return nil
		}

		return fetchCompose(source, path)
	},
}

// fetchSourceURL validates and normalises the fetch argument. Scheme-less
// repository paths on known code hosts (e.g. "github.com/org/repo") get https://.
func fetchSourceURL(source string) (*url.URL, error) {
	raw := source
	if !isURL(raw) {
		for _, host := range repoHosts {
			if strings.HasPrefix(raw, host+"/") {
				raw = "https://" + raw
				break
			}
		}
	}

	u, err := url.Parse(raw)
	if err != nil || !isURL(raw) {
		return nil, fmt.Errorf("%s is not a URL; expected a repository URL or a compose file URL", source)
	}
	if _, ok := repoComposeURLs(u); !ok && !isComposePath(u.Path) {
		return nil, fmt.Errorf(
			"%s is neither a repository URL on %s nor a .yml/.yaml URL; to import a .env file use \"envy import\"",
			source, strings.Join(repoHosts, ", "))
	}
	return u, nil
}

// fetchCompose downloads the compose file for u and writes it to path.
func fetchCompose(u *url.URL, path string) error {
	candidates, ok := repoComposeURLs(u)
	if !ok {
		candidates = []string{u.String()}
	}

	fetchedURL, body, err := fetchFirst(candidates)
	if errors.Is(err, errRemoteNotFound) {
		if ok {
			return fmt.Errorf("no %s found in the root of %s (default branch)",
				strings.Join(repoComposeFilenames, " or "), u)
		}
		return fmt.Errorf("fetching %s: %w", u, err)
	}
	if err != nil {
		return err
	}

	return writeFetchedCompose(fetchedURL, body, path)
}

// writeFetchedCompose validates fetched compose content and writes it unchanged,
// so comments and formatting survive.
func writeFetchedCompose(sourceURL string, body []byte, path string) error {
	m, err := compose.Parse(body, sourceURL)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", sourceURL, err)
	}
	color.Green("✓ Fetched %s", sourceURL)
	warnUndefinedCommandVars(m)

	if err := envfile.Write(path, string(body)); err != nil {
		return err
	}
	color.Green("✓ Written %s", path)
	return nil
}

// isComposePath reports whether a URL path points to a YAML file.
func isComposePath(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
}

// repoComposeURLs maps a repository URL on a known code host to the raw-file
// URLs of the candidate compose files on the default branch. It returns false
// when u is not a repository root URL.
func repoComposeURLs(u *url.URL) ([]string, bool) {
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	repoPath := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	segments := strings.Split(repoPath, "/")
	for _, s := range segments {
		if s == "" || s == "-" {
			return nil, false
		}
	}

	var rawURL func(name string) string
	switch host {
	case "github.com":
		if len(segments) != 2 {
			return nil, false
		}
		rawURL = func(name string) string {
			return fmt.Sprintf("https://raw.githubusercontent.com/%s/HEAD/%s", repoPath, name)
		}
	case "codeberg.org":
		if len(segments) != 2 {
			return nil, false
		}
		// Forgejo API: ref defaults to the default branch.
		rawURL = func(name string) string {
			return fmt.Sprintf("https://codeberg.org/api/v1/repos/%s/raw/%s", repoPath, name)
		}
	case "gitlab.com":
		// GitLab allows nested groups, so any depth >= 2 is a project path.
		if len(segments) < 2 {
			return nil, false
		}
		rawURL = func(name string) string {
			return fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/repository/files/%s/raw?ref=HEAD",
				url.PathEscape(repoPath), url.PathEscape(name))
		}
	default:
		return nil, false
	}

	urls := make([]string, 0, len(repoComposeFilenames))
	for _, name := range repoComposeFilenames {
		urls = append(urls, rawURL(name))
	}
	return urls, true
}

func init() {
	rootCmd.AddCommand(fetchCmd)
	fetchCmd.Flags().StringVarP(&fetchFilePath, "file", "f", compose.DefaultManifestFilename,
		"File path: folder name (creates folder and writes compose.yml) or .yaml/.yml file path (creates parent folders as needed)")
}
