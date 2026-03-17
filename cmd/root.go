package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var manifestPath string

var rootCmd = &cobra.Command{
	Use:   "envy",
	Short: "InvenioRDM environment variable manager",
	Long: `envy manages InvenioRDM environment variables via a structured
env.yaml manifest. It generates .env files, validates configuration,
produces documentation, and audits secrets.

  envy generate --no-secrets > .env.example
  envy validate --env-file .env.prod
  envy diff
  envy docs -o docs/ENV.md
  envy secrets --check`,
}

// Execute is the entry point called by main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&manifestPath, "manifest", "m", "",
		"Path to env.yaml (auto-detected if not given)",
	)
}
