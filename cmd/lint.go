package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/front-matter/envy/compose"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var lintFix bool

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Lint compose.yaml for non-fatal configuration issues",
	Long: `Lint compose.yaml for warnings such as ambiguous defaults and
invalid service-to-set references.

Examples:
  envy lint
  envy lint --manifest ./compose.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolveManifest(manifestPath)
		if err != nil {
			return err
		}

		m, err := compose.Load(path)
		if err != nil {
			return err
		}

		if lintFix {
			fixed, fixErr := applyLintFixes(path, m)
			if fixErr != nil {
				return fixErr
			}
			if fixed {
				color.Green("Applied lint fixes to %s", path)
				m, err = compose.Load(path)
				if err != nil {
					return err
				}
			}
		}

		issues := m.LintIssues()
		if len(issues) == 0 {
			color.Green("No lint findings in %s", path)
			return nil
		}

		errorCount := 0
		warningCount := 0

		for _, issue := range issues {
			if issue.Level == "error" {
				errorCount++
			} else {
				warningCount++
			}
		}

		color.Yellow("%d lint finding(s) in %s (%d error(s), %d warning(s)):", len(issues), path, errorCount, warningCount)
		for _, issue := range issues {
			line := fmt.Sprintf("  - [%s] %s", issue.Rule, issue.Message)
			if issue.Path != "" {
				line = fmt.Sprintf("%s (%s)", line, issue.Path)
			}
			if issue.Level == "error" {
				color.Red(line)
			} else {
				color.Yellow(line)
			}
		}

		if errorCount > 0 {
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(lintCmd)
	lintCmd.Flags().BoolVar(&lintFix, "fix", false, "Automatically fix supported lint warnings")
}

func applyLintFixes(path string, m *compose.Project) (bool, error) {
	if m == nil {
		return false, nil
	}

	needsSortFix := false
	for _, issue := range m.LintIssues() {
		if issue.Rule == "x-set-alphabetical-order" && issue.Level == "warning" {
			needsSortFix = true
			break
		}
	}

	if !needsSortFix {
		return false, nil
	}

	fixed, err := sortXSetBlocksInComposeYAML(path)
	if err != nil {
		return false, err
	}
	return fixed, nil
}

func sortXSetBlocksInComposeYAML(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("reading compose file %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("parsing compose file %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return false, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return false, nil
	}

	type pair struct {
		key   *yaml.Node
		value *yaml.Node
		name  string
	}

	pairs := make([]pair, 0, len(root.Content)/2)
	for i := 0; i < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		pairs = append(pairs, pair{key: k, value: v, name: k.Value})
	}

	var setPairs []pair
	firstSetIndex := -1
	for idx, p := range pairs {
		if strings.HasPrefix(p.name, "x-set-") {
			if firstSetIndex == -1 {
				firstSetIndex = idx
			}
			setPairs = append(setPairs, p)
		}
	}

	if len(setPairs) < 2 {
		return false, nil
	}

	sortedPairs := append([]pair(nil), setPairs...)
	sort.SliceStable(sortedPairs, func(i, j int) bool {
		a := sortedPairs[i].name
		b := sortedPairs[j].name
		if a == "x-set-base" && b != "x-set-base" {
			return true
		}
		if b == "x-set-base" && a != "x-set-base" {
			return false
		}
		return a < b
	})

	alreadySorted := true
	for i := range setPairs {
		if setPairs[i].name != sortedPairs[i].name {
			alreadySorted = false
			break
		}
	}
	if alreadySorted {
		return false, nil
	}

	newPairs := make([]pair, 0, len(pairs))
	inserted := false
	for idx, p := range pairs {
		if idx == firstSetIndex {
			newPairs = append(newPairs, sortedPairs...)
			inserted = true
		}
		if strings.HasPrefix(p.name, "x-set-") {
			continue
		}
		newPairs = append(newPairs, p)
	}
	if !inserted {
		newPairs = append(newPairs, sortedPairs...)
	}

	root.Content = root.Content[:0]
	for _, p := range newPairs {
		root.Content = append(root.Content, p.key, p.value)
	}

	updated, err := yaml.Marshal(&doc)
	if err != nil {
		return false, fmt.Errorf("encoding compose file %s: %w", path, err)
	}

	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return false, fmt.Errorf("writing compose file %s: %w", path, err)
	}

	return true, nil
}
