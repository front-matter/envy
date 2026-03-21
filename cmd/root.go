package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var manifestPath string

var rootCmd = &cobra.Command{
	Use:           "envy",
	Version:       Version,
	Short:         "Environment variable manager",
	SilenceErrors: true,
	Long: `envy manages environment variables via a structured compose.yaml 
compose. It generates .env files, validates configuration,
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
	envy --version
	envy server --bind 0.0.0.0
	envy deploy --target production`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return ensureRequiredGitignoreEntries()
	},
}

// Execute is the entry point called by main.
func Execute() {
	if hasVersionArg(os.Args[1:]) {
		fmt.Printf("envy version %s\n", Version)
		return
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func hasVersionArg(args []string) bool {
	for _, arg := range args {
		if arg == "--version" || arg == "-v" {
			return true
		}
	}

	return false
}

func init() {
	rootCmd.InitDefaultVersionFlag()
	if versionFlag := rootCmd.Flags().Lookup("version"); versionFlag != nil {
		versionFlag.Shorthand = "v"
	}

	rootCmd.PersistentFlags().StringVarP(
		&manifestPath, "manifest", "m", "",
		"Path to compose.yaml (auto-detected if not given)",
	)
}
