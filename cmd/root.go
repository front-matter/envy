package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var manifestPath string

var rootCmd = &cobra.Command{
	Use:           "envy",
	Short:         "Environment variable manager",
	SilenceErrors: true,
	Long: `envy manages environment variables via a structured env.yaml 
manifest. It generates .env files, validates configuration,
produces documentation, and audits secrets.

	envy import
	envy import compose.yaml --file ./generated
	envy lint
	envy diff .env.prod
	envy compose
	envy compose --flavor coolify --without-service db,cache
	envy generate --no-secrets > .env.example
	envy validate .env.prod
	envy build --destination public
	envy secrets --check
	envy server --bind 0.0.0.0
	envy deploy --target production`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return ensureRequiredGitignoreEntries()
	},
}

// Execute is the entry point called by main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&manifestPath, "manifest", "m", "",
		"Path to env.yaml (auto-detected if not given)",
	)
}
