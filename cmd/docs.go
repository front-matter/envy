package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/front-matter/envy/internal/manifest"
	"github.com/front-matter/envy/internal/renderer"
	"github.com/spf13/cobra"
)

var (
	docsOutput string
	docsFormat string
)

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Generate environment variable documentation",
	Long: `Generate documentation for all environment variables defined in env.yaml.

Examples:
  envy docs > docs/ENV.md
  envy docs -o docs/ENV.md
  envy docs --format rst -o docs/configuration.rst
  envy docs --format table`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := manifest.Load(path)
		if err != nil {
			return err
		}

		content, err := renderer.Render(m, docsFormat)
		if err != nil {
			return err
		}

		if docsOutput != "" {
			if err := os.WriteFile(docsOutput, []byte(content), 0o644); err != nil {
				return fmt.Errorf("writing docs: %w", err)
			}
			color.Green("✅ Written to %s", docsOutput)
		} else {
			fmt.Print(content)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.Flags().StringVarP(&docsOutput, "output", "o", "",
		"Write to file instead of stdout")
	docsCmd.Flags().StringVar(&docsFormat, "format", "markdown",
		"Output format: markdown, rst, table")
}
