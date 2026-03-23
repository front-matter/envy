package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/front-matter/envy/compose"
	"github.com/front-matter/envy/envfile"
	"github.com/front-matter/envy/writer"
	"github.com/spf13/cobra"
)

var (
	generateOutput string
	generateFile   string
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a .env file from compose.yaml",
	Long: `Generate a documented .env file from compose.yaml.

Examples:
	envy generate > .env.example
	envy generate --file out/
  envy generate -o .env.local`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := compose.Load(path)
		if err != nil {
			return err
		}

		content := writer.Generate(m)

		if cmd.Flags().Changed("file") && cmd.Flags().Changed("output") {
			return fmt.Errorf("use only one of --output or --file")
		}

		outputPath := generateOutput
		if cmd.Flags().Changed("file") {
			outputPath, err = resolveCommandFilePath(generateFile, ".env")
			if err != nil {
				return err
			}
		}

		if outputPath != "" {
			if _, err := os.Stat(outputPath); err == nil {
				color.Yellow("Warning: %s already exists; not writing file.", outputPath)
				return nil
			}
			if err := envfile.Write(outputPath, content); err != nil {
				return err
			}
			color.Green("✅ Written to %s", outputPath)
		} else {
			fmt.Print(content)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(generateCmd)
	generateCmd.Flags().StringVarP(&generateOutput, "output", "o", "",
		"Write to file instead of stdout")
	generateCmd.Flags().StringVarP(&generateFile, "file", "f", "",
		"File path: folder name (creates folder and writes .env) or file path")
}
