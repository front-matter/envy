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
	Short:         "Docker Compose manager",
	SilenceErrors: true,
	Long: `envy manages Docker Compose files. It validates and lints them,
	manages Compose profiles, generates and diffs .env files, and 
produces documentation that can be deployed as a static website. Example usage:

envy validate
envy lint
envy import
	envy generate > .env.example
envy diff .env.prod
envy build --destination public
envy server --bind 0.0.0.0
envy deploy --target production
envy --version`,
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
		&manifestPath, "file", "f", "",
		"Path to compose file (auto-detected: compose.yml, compose.yaml, docker-compose.yml, docker-compose.yaml)",
	)
}
