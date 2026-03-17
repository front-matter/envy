package cmd

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/front-matter/envy/internal/envfile"
	"github.com/front-matter/envy/internal/generator"
	"github.com/front-matter/envy/internal/manifest"
	"github.com/spf13/cobra"
)

var (
	generateNoSecrets bool
	generateOutput    string
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a .env file from env.yaml",
	Long: `Generate a documented .env file from env.yaml.

Examples:
  envy generate --no-secrets > .env.example
  envy generate -o .env.local`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := manifest.Load(path)
		if err != nil {
			return err
		}

		content := generator.Generate(m, generator.Options{
			IncludeSecrets: !generateNoSecrets,
		})

		if generateOutput != "" {
			if err := envfile.Write(generateOutput, content); err != nil {
				return err
			}
			color.Green("✅ Written to %s", generateOutput)
		} else {
			fmt.Print(content)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(generateCmd)
	generateCmd.Flags().BoolVar(&generateNoSecrets, "no-secrets", false,
		"Omit secret values (safe for .env.example)")
	generateCmd.Flags().StringVarP(&generateOutput, "output", "o", "",
		"Write to file instead of stdout")
}
