package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/front-matter/envy/compose"
	"github.com/front-matter/envy/envfile"
	"github.com/front-matter/envy/reader"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	importFilePath       string
	commandVarRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\}`)
)

var importCmd = &cobra.Command{
	Use:   "import [path]",
	Short: "Import .env files to generate compose.yml",
	Long: `Import .env files and convert them into a compose.yml compose.

Auto-detection: If no files are specified, the command looks for .env and .env.example
in the current directory.

File paths: The --file flag can be either a folder or a file path ending in .yaml/.yml.
- Folder: creates the folder if needed and writes to folder/compose.yml
- File: .yaml/.yml file path, creates parent directories as needed

Examples:
  envy import
	  envy import .env
	  envy import ./config
	  envy import --file config/
	  envy import --file config.yaml`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve destination path (handles folder or file path)
		path, err := resolvePath(importFilePath)
		if err != nil {
			return err
		}

		sourcePath := ""
		if len(args) == 1 {
			sourcePath = args[0]
		}

		// Determine which files to import
		filesToImport, err := resolveImportPaths(sourcePath)
		if err != nil {
			return err
		}

		if len(filesToImport) == 0 {
			return fmt.Errorf("no import files found (tried auto-detection of .env and .env.example)")
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

		// Color output for detected files
		if sourcePath == "" {
			color.Cyan("Auto-detected %d file(s) for import:", len(filesToImport))
			for _, f := range filesToImport {
				color.Cyan("  - %s", f)
			}
		}

		// Import manifests
		var manifests []*compose.Project
		for _, importPath := range filesToImport {
			m, err := importFile(importPath)
			if err != nil {
				return fmt.Errorf("importing %s: %w", importPath, err)
			}
			manifests = append(manifests, m)
			color.Green("✓ Imported %s", importPath)
		}

		// Merge manifests
		merged := reader.Merge(manifests...)

		for _, w := range verifyServiceCommandVarsDefined(merged) {
			color.Yellow("Warning: %s", w)
		}

		// Marshal and write
		content, err := yaml.Marshal(filterSecretVars(merged))
		if err != nil {
			return fmt.Errorf("rendering manifest YAML: %w", err)
		}

		if err := envfile.Write(path, string(content)); err != nil {
			return err
		}

		color.Green("✓ Written %s", path)
		return nil
	},
}

// importFile detects file type and imports accordingly.
func importFile(path string) (*compose.Project, error) {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".env") || strings.HasSuffix(lower, ".env.example") || strings.HasSuffix(lower, ".env.local") {
		return reader.ImportEnvFile(path)
	}
	return nil, fmt.Errorf("unsupported file type: %s (expected .env, .env.example, or .env.local)", path)
}

// resolvePath determines the final file path.
// If the path ends with .yaml or .yml, it's used as the file path.
// If the path is a folder (or doesn't have a yaml extension), it creates the folder
// and returns path/compose.yml.
// Rejects any other file extensions.
func resolvePath(path string) (string, error) {
	lower := strings.ToLower(path)

	// Check if it's a file with valid extension
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		// Valid yaml file path
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			// Create directory if it doesn't exist
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("creating directory %s: %w", dir, err)
			}
		}
		return path, nil
	}

	// Check if it has an invalid extension
	if strings.Contains(path, ".") && !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
		ext := filepath.Ext(path)
		return "", fmt.Errorf("invalid file extension %q (must be .yaml or .yml)", ext)
	}

	// Treat as a folder path
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", path, err)
	}

	return filepath.Join(path, compose.DefaultManifestFilename), nil
}

func FileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return false, fmt.Errorf("path %s is a directory", path)
		}
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, fmt.Errorf("checking file %s: %w", path, err)
}

// resolveImportPaths determines which files to import.
// If sourcePath is empty, auto-detects supported env files in current directory.
// If sourcePath is a directory, finds supported env files within it.
// If sourcePath is a file, imports that file.
// Returns a deduplicated slice of file paths.
func resolveImportPaths(sourcePath string) ([]string, error) {
	if sourcePath == "" {
		// Auto-detect in current directory
		return autoDetectImportFiles(".")
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", sourcePath, err)
	}

	if info.IsDir() {
		return findImportFiles(sourcePath)
	}

	return []string{sourcePath}, nil
}

// autoDetectImportFiles looks for supported env files in a directory.
func autoDetectImportFiles(dir string) ([]string, error) {
	return findImportFiles(dir)
}

// findImportFiles searches for importable files in a directory using a fixed priority order.
func findImportFiles(dir string) ([]string, error) {
	var result []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}

	available := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		available[strings.ToLower(entry.Name())] = entry.Name()
	}

	envCandidates := []string{".env", ".env.example"}
	for _, candidate := range envCandidates {
		if actualName, ok := available[candidate]; ok {
			result = append(result, filepath.Join(dir, actualName))
			break
		}
	}

	return result, nil
}

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.Flags().StringVarP(&importFilePath, "file", "f", compose.DefaultManifestFilename,
		"File path: folder name (creates folder and writes compose.yml) or .yaml/.yml file path (creates parent folders as needed)")
}

func verifyServiceCommandVarsDefined(m *compose.Project) []string {
	if m == nil {
		return nil
	}

	allVars := make(map[string]struct{})
	for _, v := range m.AllVars() {
		allVars[v.Key] = struct{}{}
	}

	var issues []string
	for _, svc := range m.Services {
		if len(svc.Command) == 0 {
			continue
		}

		missing := make(map[string]struct{})
		for _, token := range svc.Command {
			for _, match := range commandVarRefPattern.FindAllStringSubmatch(token, -1) {
				if len(match) < 2 {
					continue
				}
				varName := match[1]
				if _, ok := allVars[varName]; !ok {
					missing[varName] = struct{}{}
				}
			}
		}

		if len(missing) == 0 {
			continue
		}

		missingList := make([]string, 0, len(missing))
		for key := range missing {
			missingList = append(missingList, key)
		}
		sort.Strings(missingList)

		issues = append(issues, fmt.Sprintf(
			"service %q command references undefined vars: %s",
			svc.Name,
			strings.Join(missingList, ", "),
		))
	}

	return issues
}

// filterSecretVars removes vars marked as secret from sets before writing compose.yaml.
func filterSecretVars(m *compose.Project) *compose.Project {
	if m == nil {
		return nil
	}

	out := *m
	out.Services = append([]compose.Service(nil), m.Services...)
	out.SetVolumeNames(append([]string(nil), m.VolumeNames()...))
	out.SetNetworkNames(append([]string(nil), m.NetworkNames()...))

	out.Sets = make(map[string]compose.Set, len(m.Sets))
	for setKey, set := range m.Sets {
		filteredVars := make([]compose.Var, 0, len(set.Vars))
		for _, v := range set.Vars {
			if v.IsSecret() {
				continue
			}
			filteredVars = append(filteredVars, v)
		}

		set.Vars = filteredVars
		out.Sets[setKey] = set
	}

	return &out
}
