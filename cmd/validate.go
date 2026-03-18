package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/front-matter/envy/envfile"
	"github.com/front-matter/envy/manifest"
	"github.com/front-matter/envy/validator"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate a .env file against env.yaml",
	Long: `Validate environment variables against the env.yaml schema.
Checks required fields.

Examples:
  envy validate
	  envy validate .env.prod
	  envy validate ./config`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := manifest.Load(path)
		if err != nil {
			return err
		}

		envPath, err := resolveEnvInputPath(args)
		if err != nil {
			return err
		}

		ef, err := envfile.Load(envPath)
		if err != nil {
			return err
		}

		errs := validator.Validate(m, ef.Values)

		if len(errs) > 0 {
			color.Red("\n❌ %d validation error(s) in %s:\n", len(errs), envPath)
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

		color.Green("\n✅ %s is valid (%d vars checked)\n", envPath, len(ef.Values))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
