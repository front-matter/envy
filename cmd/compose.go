package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/front-matter/envy/compose"
	"github.com/front-matter/envy/envfile"
	"github.com/front-matter/envy/writer"
	"github.com/spf13/cobra"
)

var (
	composeOutput         string
	composeFile           string
	composeService        string
	composeWithoutService string
	composeFlavor         string
)

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Generate a Docker Compose file from compose.yaml",
	Long: `Generate a Docker Compose file with environment variables derived
from compose.yaml defaults.

Examples:
  envy compose
	  envy compose --flavor coolify
	  envy compose --without-service db,cache
	  envy compose --service web
	  envy compose --file out/
  envy compose -o docker-compose.yml
	  envy compose --service web --without-service db,cache`,

	RunE: func(cmd *cobra.Command, args []string) error {
		if composeFlavor != "default" && composeFlavor != "coolify" {
			return fmt.Errorf("unknown flavor %q - use default or coolify", composeFlavor)
		}

		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := compose.Load(path)
		if err != nil {
			return err
		}

		defaultFilename := "compose.yaml"
		if composeFlavor == "coolify" {
			defaultFilename = "compose.coolify.yaml"
		}

		outputPath := composeOutput
		if !cmd.Flags().Changed("output") {
			outputPath = defaultFilename
		}
		if cmd.Flags().Changed("file") {
			if cmd.Flags().Changed("output") {
				return fmt.Errorf("use only one of --output or --file")
			}

			outputPath, err = resolvePath(composeFile)
			if err != nil {
				return err
			}
		}

		content := writer.GenerateCompose(m, writer.ComposeOptions{
			ServiceName:     composeService,
			ExcludeServices: splitCommaList(composeWithoutService),
			Flavor:          composeFlavor,
		})

		if _, err := os.Stat(outputPath); err == nil {
			color.Yellow("Warning: %s already exists; not writing file.", outputPath)
			return nil
		}

		if err := envfile.Write(outputPath, content); err != nil {
			return err
		}

		color.Green("Written %s", outputPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(composeCmd)
	composeCmd.Flags().StringVarP(&composeOutput, "output", "o", "",
		"Path to Docker Compose output file (default: compose.yaml, compose.coolify.yaml for coolify flavor)")
	composeCmd.Flags().StringVarP(&composeFile, "file", "f", "",
		"File path: folder name (creates folder and writes compose.yaml) or .yaml/.yml file path")
	composeCmd.Flags().StringVar(&composeService, "service", "",
		"Emit only this service (default: all services from compose.yaml)")
	composeCmd.Flags().StringVar(&composeWithoutService, "without-service", "",
		"Comma-separated list of services to omit from the compose file")
	composeCmd.Flags().StringVar(&composeFlavor, "flavor", "default",
		"Compose flavor to generate: default, coolify")
}

func splitCommaList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}

	return result
}
