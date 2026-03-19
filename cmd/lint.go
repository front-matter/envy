package cmd

import (
	"fmt"

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

		warnings := m.Lint()
		if len(warnings) == 0 {
			color.Green("No lint warnings in %s", path)
			return nil
		}

		color.Yellow("%d lint warning(s) in %s:", len(warnings), path)
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(lintCmd)
}
