package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/front-matter/envy/manifest"
	"github.com/front-matter/envy/renderer"
	"github.com/spf13/cobra"
)

var (
	docsOutput string
	docsFile   string
	docsFormat string
)

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Generate environment variable documentation",
	Long: `Generate documentation for all environment variables defined in env.yaml.

Examples:
  envy docs > docs/ENV.md
	envy docs --file docs/
  envy docs -o docs/ENV.md
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

		if cmd.Flags().Changed("file") && cmd.Flags().Changed("output") {
			return fmt.Errorf("use only one of --output or --file")
		}

		outputPath := docsOutput
		if cmd.Flags().Changed("file") {
			outputPath, err = resolveCommandFilePath(docsFile, "ENV.md")
			if err != nil {
				return err
			}
		}

		if outputPath != "" {
			if err := os.WriteFile(outputPath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("writing docs: %w", err)
			}
			color.Green("✅ Written to %s", outputPath)
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
	docsCmd.Flags().StringVarP(&docsFile, "file", "f", "",
		"File path: folder name (creates folder and writes ENV.md) or file path")
	docsCmd.Flags().StringVar(&docsFormat, "format", "markdown",
		"Output format: markdown, table")
}
