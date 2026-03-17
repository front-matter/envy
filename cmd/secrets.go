package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/front-matter/envy/internal/manifest"
	"github.com/spf13/cobra"
)

var secretsCheck bool

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "List or audit secret environment variables",
	Long: `List all variables marked as secret in env.yaml.
With --check, scans git-tracked files for exposed secret values.

Examples:
  envy secrets
  envy secrets --check`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := manifest.Load(path)
		if err != nil {
			return err
		}

		if secretsCheck {
			return runSecretsCheck(m)
		}

		fmt.Print("\nSecret variables (must not appear plaintext in git):\n\n")
		for _, v := range m.SecretVars() {
			color.Red("  🔒 %s", v.Key)
			desc := strings.ReplaceAll(strings.TrimSpace(v.Description), "\n", " ")
			if len(desc) > 80 {
				desc = desc[:80] + "..."
			}
			fmt.Printf("     %s\n", desc)
		}
		fmt.Println()
		return nil
	},
}

func runSecretsCheck(m *manifest.Manifest) error {
	var secretKeys []string
	for _, v := range m.SecretVars() {
		secretKeys = append(secretKeys, v.Key)
	}

	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		return fmt.Errorf("git ls-files failed (not a git repo?): %w", err)
	}

	files := strings.Split(strings.TrimSpace(string(out)), "\n")

	var violations []string
	for _, file := range files {
		// Skip encrypted files
		if strings.HasSuffix(file, ".enc") ||
			strings.Contains(file, ".sops.") ||
			strings.HasSuffix(file, ".enc.yaml") ||
			strings.HasSuffix(file, ".enc.env") {
			continue
		}

		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)

		for _, key := range secretKeys {
			// Match KEY=value where value is non-empty and not a placeholder
			pattern := fmt.Sprintf(`(?m)^%s=(?!CHANGE_ME|your[-_]|<|ENC\[|$).+`,
				regexp.QuoteMeta(key))
			matched, _ := regexp.MatchString(pattern, content)
			if matched {
				violations = append(violations, fmt.Sprintf("%s: %s", file, key))
			}
		}
	}

	if len(violations) > 0 {
		color.Red("\n⚠️  %d secret(s) may be exposed in git-tracked files:\n",
			len(violations))
		for _, v := range violations {
			color.Red("  %s", v)
		}
		fmt.Println()
		os.Exit(1)
	}

	color.Green("\n✅ No secrets found in tracked files.\n")
	return nil
}

func init() {
	rootCmd.AddCommand(secretsCmd)
	secretsCmd.Flags().BoolVar(&secretsCheck, "check", false,
		"Scan git-tracked files for exposed secret values")
}
