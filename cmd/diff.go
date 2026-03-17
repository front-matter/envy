package cmd

import (
	"fmt"
	"sort"

	"github.com/fatih/color"
	"github.com/front-matter/envy/internal/envfile"
	"github.com/front-matter/envy/internal/manifest"
	"github.com/spf13/cobra"
)

var diffEnvFile string

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show variables missing from or extra in a .env file",
	Long: `Compare env.yaml against a .env file.
Reports variables defined in the manifest but absent from the file,
and variables present in the file but not in the manifest.

Example:
  envy diff --env-file .env.prod`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := manifest.Load(path)
		if err != nil {
			return err
		}

		ef, err := envfile.Load(diffEnvFile)
		if err != nil {
			return err
		}

		manifestKeys := make(map[string]bool)
		for _, v := range m.AllVars() {
			manifestKeys[v.Key] = true
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
			color.Green("\n✅ %s matches env.yaml exactly.\n", diffEnvFile)
			return nil
		}

		if len(missing) > 0 {
			color.Yellow("\n📋 In env.yaml but missing from %s:", diffEnvFile)
			for _, k := range missing {
				fmt.Printf("   - %s\n", k)
			}
		}

		if len(extra) > 0 {
			color.Cyan("\n➕ In %s but not in env.yaml:", diffEnvFile)
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
	diffCmd.Flags().StringVarP(&diffEnvFile, "env-file", "e", ".env",
		"Path to .env file to compare")
}
