// Package reader converts external config formats into envy manifests.
package reader

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/front-matter/envy/manifest"
	"github.com/getsops/sops/v3/decrypt"
	"gopkg.in/yaml.v3"
)

var interpolationPattern = regexp.MustCompile(`^\$\{([^}:]+)(?:(:?[-?])(.*))?\}$`)
var decryptComposeFile = func(path string) ([]byte, error) {
	return decrypt.File(path, "yaml")
}

// ImportCompose reads a compose file and converts it to an envy manifest.
func ImportCompose(path string) (*manifest.Manifest, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving compose file path %s: %w", path, err)
	}

	composeContent, err := loadComposeContent(absPath)
	if err != nil {
		return nil, err
	}

	configDetails := types.ConfigDetails{
		WorkingDir: filepath.Dir(absPath),
		ConfigFiles: []types.ConfigFile{{
			Filename: absPath,
			Content:  composeContent,
		}},
	}

	project, err := loader.LoadWithContext(context.Background(), configDetails, func(o *loader.Options) {
		o.SkipInterpolation = true
		o.SkipResolveEnvironment = true
		o.SkipNormalization = true
		o.SetProjectName(filepath.Base(filepath.Dir(absPath)), false)
	})
	if err != nil {
		return nil, fmt.Errorf("parsing compose file %s: %w", path, err)
	}

	if len(project.Services) == 0 && len(project.Volumes) == 0 && len(project.Networks) == 0 {
		return nil, fmt.Errorf("compose file %s has no services, volumes, or networks", path)
	}

	serviceNames := sortedKeys(map[string]types.ServiceConfig(project.Services))
	volumes := sortedKeys(project.Volumes)
	networks := sortedKeys(project.Networks)

	metaName, metaDescription := extractComposeHeaderComments(composeContent)
	if metaName == "" {
		metaName = "Imported Compose Manifest"
	}

	m := &manifest.Manifest{
		Meta: manifest.Meta{
			Name:        metaName,
			Description: metaDescription,
			Version:     "v1",
		},
		Services: make([]manifest.Service, 0, len(serviceNames)),
		Groups:   make(map[string]manifest.Group),
		Volumes:  volumes,
		Networks: networks,
	}

	for _, serviceName := range serviceNames {
		svc := project.Services[serviceName]
		groupKey := serviceGroupKey(serviceName)

		vars := composeEnvToVars(svc.Environment)

		m.Services = append(m.Services, manifest.Service{
			Name:       serviceName,
			Image:      strings.TrimSpace(svc.Image),
			Platform:   strings.TrimSpace(svc.Platform),
			Entrypoint: []string(svc.Entrypoint),
			Command:    []string(svc.Command),
			Groups:     []string{groupKey},
		})

		m.Groups[groupKey] = manifest.Group{
			Description: fmt.Sprintf("Imported environment for service %s", serviceName),
			Vars:        vars,
		}
	}

	consolidateCommonGroupVars(m)

	return m, nil
}

func consolidateCommonGroupVars(m *manifest.Manifest) {
	if m == nil || len(m.Services) < 2 {
		return
	}

	varDefinitions := make(map[string]manifest.Var)
	varGroups := make(map[string][]string)
	varConflicts := make(map[string]bool)

	for groupKey, group := range m.Groups {
		seenInGroup := make(map[string]bool)
		for _, variable := range group.Vars {
			if seenInGroup[variable.Key] {
				continue
			}
			seenInGroup[variable.Key] = true

			if existing, ok := varDefinitions[variable.Key]; ok {
				if !sameManifestVar(existing, variable) {
					varConflicts[variable.Key] = true
				}
			} else {
				varDefinitions[variable.Key] = variable
			}

			varGroups[variable.Key] = append(varGroups[variable.Key], groupKey)
		}
	}

	commonKeys := make([]string, 0)
	commonKeySet := make(map[string]bool)
	for key, groups := range varGroups {
		if len(groups) > 1 && !varConflicts[key] {
			commonKeys = append(commonKeys, key)
			commonKeySet[key] = true
		}
	}

	if len(commonKeys) == 0 {
		return
	}
	sort.Strings(commonKeys)

	affectedGroups := make(map[string]bool)
	for groupKey, group := range m.Groups {
		filtered := make([]manifest.Var, 0, len(group.Vars))
		removedAny := false
		for _, variable := range group.Vars {
			if commonKeySet[variable.Key] {
				removedAny = true
				continue
			}
			filtered = append(filtered, variable)
		}

		if removedAny {
			affectedGroups[groupKey] = true
		}

		if len(filtered) == 0 {
			delete(m.Groups, groupKey)
			continue
		}

		group.Vars = filtered
		m.Groups[groupKey] = group
	}

	commonVars := make([]manifest.Var, 0, len(commonKeys))
	for _, key := range commonKeys {
		commonVars = append(commonVars, varDefinitions[key])
	}

	if existing, ok := m.Groups["common"]; ok {
		existing.Description = "Shared environment for multiple services"
		existing.Vars = mergeVars(existing.Vars, commonVars)
		m.Groups["common"] = existing
	} else {
		m.Groups["common"] = manifest.Group{
			Description: "Shared environment for multiple services",
			Vars:        commonVars,
		}
	}

	for index, service := range m.Services {
		updatedGroups := make([]string, 0, len(service.Groups)+1)
		needsCommon := false
		for _, groupKey := range service.Groups {
			if affectedGroups[groupKey] {
				needsCommon = true
			}
			if _, ok := m.Groups[groupKey]; ok {
				updatedGroups = append(updatedGroups, groupKey)
			}
		}
		if needsCommon {
			updatedGroups = append([]string{"common"}, updatedGroups...)
		}
		service.Groups = dedupePreserveOrder(updatedGroups)
		m.Services[index] = service
	}
}

func sameManifestVar(left, right manifest.Var) bool {
	return left.Key == right.Key &&
		left.Description == right.Description &&
		left.Default.String() == right.Default.String() &&
		left.Required == right.Required &&
		left.Secret == right.Secret &&
		left.Example == right.Example
}

func dedupePreserveOrder(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func loadComposeContent(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading compose file %s: %w", path, err)
	}

	if !hasSOPSMetadata(content) {
		return content, nil
	}

	decrypted, err := decryptComposeFile(path)
	if err != nil {
		return nil, fmt.Errorf("decrypting compose file %s with sops: %w", path, err)
	}

	return decrypted, nil
}

func hasSOPSMetadata(content []byte) bool {
	var document map[string]interface{}
	if err := yaml.Unmarshal(content, &document); err != nil {
		return false
	}

	_, ok := document["sops"]
	return ok
}

// extractComposeHeaderComments reads comment lines at the top of a compose file
// that appear before the first non-comment, non-empty line.
// The first comment line becomes the name; the rest become the description.
func extractComposeHeaderComments(content []byte) (name, description string) {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		text := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		if text != "" {
			lines = append(lines, text)
		}
	}

	if len(lines) == 0 {
		return "", ""
	}
	if len(lines) == 1 {
		return lines[0], ""
	}
	return lines[0], strings.Join(lines[1:], "\n")
}

// isSecretVar checks if a variable name suggests it should be marked as secret.
func isSecretVar(key string) bool {
	upper := strings.ToUpper(key)
	return strings.Contains(upper, "_SECRET") || strings.Contains(upper, "_PASSWORD")
}

func serviceGroupKey(serviceName string) string {
	serviceName = strings.ToLower(strings.TrimSpace(serviceName))
	if serviceName == "" {
		return "service"
	}

	var b strings.Builder
	for _, r := range serviceName {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		default:
			b.WriteRune('-')
		}
	}

	key := strings.Trim(b.String(), "-")
	if key == "" {
		return "service"
	}
	return key
}

func composeEnvToVars(env types.MappingWithEquals) []manifest.Var {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	vars := make([]manifest.Var, 0, len(keys))
	for _, key := range keys {
		var rawValue string
		if v := env[key]; v != nil {
			rawValue = *v
		} else {
			// KEY form: value comes from host env at runtime, no default.
			rawValue = "${" + key + "}"
		}
		defaultValue, _, required := parseComposeEnvValue(key, rawValue)

		vars = append(vars, manifest.Var{
			Key:         key,
			Default:     manifest.ScalarValue(defaultValue),
			Description: "Imported from compose environment",
			Required:    required,
			Secret:      isSecretVar(key),
		})
	}

	return vars
}

func parseComposeEnvValue(key, value string) (defaultValue string, editable bool, required bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false, false
	}

	matches := interpolationPattern.FindStringSubmatch(value)
	if matches == nil {
		return value, false, false
	}

	editable = true
	operator := matches[2]
	defaultValue = matches[3]

	switch operator {
	case ":?", "?":
		required = true
	case ":-", "-":
		required = false
	default:
		required = true
		defaultValue = ""
	}

	if strings.TrimSpace(matches[1]) == "" {
		defaultValue = ""
	}

	if defaultValue == "" && strings.Contains(value, "${"+key+"}") {
		required = true
	}

	return defaultValue, editable, required
}

func sortedKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
