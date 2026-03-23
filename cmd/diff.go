package cmd

import (
	"fmt"
	"sort"

	"github.com/fatih/color"
	"github.com/front-matter/envy/compose"
	"github.com/front-matter/envy/envfile"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff [path]",
	Short: "Show variables missing from or extra in a .env file",
	Long: `Compare compose.yaml against a .env file.
Reports variables defined in the manifest but absent from the file,
and variables present in the file but not in the compose.

Examples:
	  envy diff
	  envy diff .env.prod
	  envy diff ./config`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := compose.Load(path)
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

		manifestKeys := make(map[string]bool)
		for key := range m.AllVars() {
			manifestKeys[key] = true
		}

		envKeys := make(map[string]bool)
		for _, k := range ef.Keys {
			envKeys[k] = true
		}

		var missing, extra []string
		for k := range manifestKeys {
			if !envKeys[k] {
				missing = append(missing, k)
			}
		}
		for k := range envKeys {
			if !manifestKeys[k] {
				extra = append(extra, k)
			}
		}

		sort.Strings(missing)
		sort.Strings(extra)

		if len(missing) == 0 && len(extra) == 0 {
			color.Green("\n✅ %s matches compose.yaml exactly.\n", envPath)
			return nil
		}

		if len(missing) > 0 {
			color.Yellow("\n⚠️  In compose.yaml but missing from %s:", envPath)
			for _, k := range missing {
				fmt.Printf("   - %s\n", k)
			}
		}

		if len(extra) > 0 {
			color.Yellow("\n⚠️ In %s but not in compose.yaml:", envPath)
			for _, k := range extra {
				fmt.Printf("   + %s\n", k)
			}
		}

		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)
}
