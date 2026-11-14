package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/front-matter/envy/reader"
)

// maxRemoteBytes caps the size of a file fetched from a URL.
const maxRemoteBytes = 1 << 20

var (
	remoteHTTPClient = &http.Client{Timeout: 30 * time.Second}

	// errRemoteNotFound is returned by fetchURL when the server responds 404.
	errRemoteNotFound = errors.New("not found")
)

// isURL reports whether s is an http or https URL.
func isURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// importEnvURL fetches a raw .env file from rawURL, converts it and writes the
// manifest to path. Compose files and repository URLs are left to envy fetch.
func importEnvURL(rawURL, path string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parsing URL %s: %w", rawURL, err)
	}
	if _, ok := repoComposeURLs(u); ok || isComposePath(u.Path) {
		return fmt.Errorf("%s is a compose file or repository URL; use \"envy fetch %s\"", rawURL, rawURL)
	}

	body, err := fetchURL(rawURL)
	if err != nil {
		return err
	}

	m, err := reader.ImportEnv(bytes.NewReader(body), rawURL)
	if err != nil {
		return fmt.Errorf("importing %s: %w", rawURL, err)
	}
	color.Green("✓ Imported %s", rawURL)
	return writeManifest(path, m)
}

// fetchFirst fetches each URL in turn and returns the first one that exists.
// It returns errRemoteNotFound when none do.
func fetchFirst(urls []string) (string, []byte, error) {
	for _, u := range urls {
		body, err := fetchURL(u)
		if errors.Is(err, errRemoteNotFound) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return u, body, nil
	}
	return "", nil, errRemoteNotFound
}

// fetchURL GETs rawURL and returns its body, rejecting HTML pages and
// responses larger than maxRemoteBytes.
func fetchURL(rawURL string) ([]byte, error) {
	resp, err := remoteHTTPClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, errRemoteNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: unexpected status %s", rawURL, resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		return nil, fmt.Errorf("fetching %s: got HTML, expected a raw file (use the raw file URL)", rawURL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rawURL, err)
	}
	if len(body) > maxRemoteBytes {
		return nil, fmt.Errorf("fetching %s: response exceeds %d bytes", rawURL, maxRemoteBytes)
	}
	return body, nil
}
