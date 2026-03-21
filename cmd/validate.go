package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var composeConfigRunner = runComposeConfigCLI

var validateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate env.yaml as a valid Docker Compose file",
	Long: `Validate env.yaml by running "compose config".
This uses the local Docker Compose CLI and fails if the file is not a valid Compose configuration.

Examples:
  envy validate
	  envy validate env.yaml
	  envy validate ./config/env.yaml`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveValidateComposePath(args)
		if err != nil {
			return err
		}

		if err := validateComposeFile(path); err != nil {
			return err
		}

		color.Green("\n✅ %s is a valid Compose file\n", path)
		return nil
	},
}

func resolveValidateComposePath(args []string) (string, error) {
	if len(args) > 0 {
		candidate := args[0]
		if strings.HasSuffix(candidate, string(filepath.Separator)) {
			candidate = filepath.Join(candidate, "env.yaml")
		}
		return candidate, nil
	}

	return resolveManifest(manifestPath)
}

func validateComposeFile(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving compose file path %s: %w", path, err)
	}

	output, err := composeConfigRunner(absPath)
	if err != nil {
		if strings.TrimSpace(output) != "" {
			return fmt.Errorf("invalid compose file %s: %w\n%s", path, err, strings.TrimSpace(output))
		}
		return fmt.Errorf("invalid compose file %s: %w", path, err)
	}

	return nil
}

func runComposeConfigCLI(path string) (string, error) {
	if _, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command("docker", "compose", "-f", path, "config", "-q")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if _, err := exec.LookPath("docker-compose"); err == nil {
		cmd := exec.Command("docker-compose", "-f", path, "config", "-q")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	return "", fmt.Errorf("docker compose CLI not found (tried 'docker compose' and 'docker-compose')")
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
