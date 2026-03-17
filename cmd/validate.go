package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/front-matter/envy/internal/envfile"
	"github.com/front-matter/envy/internal/manifest"
	"github.com/front-matter/envy/internal/validator"
	"github.com/spf13/cobra"
)

var validateEnvFile string

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a .env file against env.yaml",
	Long: `Validate environment variables against the env.yaml schema.
Checks required fields, types, allowed values, and minimum lengths.

Examples:
  envy validate
  envy validate --env-file .env.prod`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := manifest.Load(path)
		if err != nil {
			return err
		}

		ef, err := envfile.Load(validateEnvFile)
		if err != nil {
			return err
		}

		errs := validator.Validate(m, ef.Values)

		if len(errs) > 0 {
			color.Red("\n❌ %d validation error(s) in %s:\n", len(errs), validateEnvFile)
			for _, e := range errs {
				if e.Level == "MISSING" {
					color.Red("  %s", e)
				} else {
					color.Yellow("  %s", e)
				}
			}
			fmt.Println()
			os.Exit(1)
		}

		color.Green("\n✅ %s is valid (%d vars checked)\n", validateEnvFile, len(ef.Values))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
	validateCmd.Flags().StringVarP(&validateEnvFile, "env-file", "e", ".env",
		"Path to .env file to validate")
}
