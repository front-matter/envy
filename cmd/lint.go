package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/front-matter/envy/manifest"
	"github.com/spf13/cobra"
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Lint env.yaml for non-fatal configuration issues",
	Long: `Lint env.yaml for warnings such as ambiguous defaults and
invalid service-to-set references.

Examples:
  envy lint
  envy lint --manifest ./env.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := manifest.Load(path)
		if err != nil {
			return err
		}

		issues := m.LintIssues()
		if len(issues) == 0 {
			color.Green("No lint findings in %s", path)
			return nil
		}

		errorCount := 0
		warningCount := 0

		for _, issue := range issues {
			if issue.Level == "error" {
				errorCount++
			} else {
				warningCount++
			}
		}

		color.Yellow("%d lint finding(s) in %s (%d error(s), %d warning(s)):", len(issues), path, errorCount, warningCount)
		for _, issue := range issues {
			line := fmt.Sprintf("  - [%s] %s", issue.Rule, issue.Message)
			if issue.Path != "" {
				line = fmt.Sprintf("%s (%s)", line, issue.Path)
			}
			if issue.Level == "error" {
				color.Red(line)
			} else {
				color.Yellow(line)
			}
		}

		if errorCount > 0 {
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(lintCmd)
}
