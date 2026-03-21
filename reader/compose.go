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
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/front-matter/envy/compose"
)

var interpolationPattern = regexp.MustCompile(`^\$\{([^}:]+)(?:(:?[-?])(.*))?\}$`)

// ImportCompose reads a compose file and converts it to an envy compose.
func ImportCompose(path string) (*compose.Project, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving compose file path %s: %w", path, err)
	}

	composeContent, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading compose file %s: %w", path, err)
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
		o.SkipConsistencyCheck = true
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

	metaTitle, metaDescription := extractComposeHeaderComments(composeContent)
	if metaTitle == "" {
		metaTitle = "Imported Compose Manifest"
	}

	m := &compose.Project{
		Meta: compose.Meta{
			Title:        metaTitle,
			Description:  metaDescription,
			LanguageCode: "en-US",
			Version:      "v1",
		},
		Services: make([]compose.Service, 0, len(serviceNames)),
		Sets:        make(map[string]compose.Set),
	}
	m.SetVolumeNames(volumes)
	m.SetNetworkNames(networks)

	for _, serviceName := range serviceNames {
		svc := project.Services[serviceName]
		setKey := serviceSetKey(serviceName)

		vars := composeEnvToVars(svc.Environment)

		m.Services = append(m.Services, compose.Service{
			Name:       serviceName,
			Image:      strings.TrimSpace(svc.Image),
			Platform:   strings.TrimSpace(svc.Platform),
			Entrypoint: []string(svc.Entrypoint),
			Command:    []string(svc.Command),
			Sets:       []string{setKey},
		})

		m.Sets[setKey] = compose.Set{
			Description: fmt.Sprintf("Imported environment for service %s", serviceName),
			Vars:        vars,
		}
	}

	consolidateCommonSetVars(m)

	return m, nil
}

func consolidateCommonSetVars(m *compose.Project) {
	if m == nil || len(m.Services) < 2 {
		return
	}

	varDefinitions := make(map[string]compose.Var)
	varSets := make(map[string][]string)
	varConflicts := make(map[string]bool)

	for setKey, set := range m.Sets {
		seenInSet := make(map[string]bool)
		for _, variable := range set.Vars {
			if seenInSet[variable.Key] {
				continue
			}
			seenInSet[variable.Key] = true

			if existing, ok := varDefinitions[variable.Key]; ok {
				if !sameManifestVar(existing, variable) {
					varConflicts[variable.Key] = true
				}
			} else {
				varDefinitions[variable.Key] = variable
			}

			varSets[variable.Key] = append(varSets[variable.Key], setKey)
		}
	}

	commonKeys := make([]string, 0)
	commonKeySet := make(map[string]bool)
	for key, sets := range varSets {
		if len(sets) > 1 && !varConflicts[key] {
			commonKeys = append(commonKeys, key)
			commonKeySet[key] = true
		}
	}

	if len(commonKeys) == 0 {
		return
	}
	sort.Strings(commonKeys)

	affectedSets := make(map[string]bool)
	for setKey, set := range m.Sets {
		filtered := make([]compose.Var, 0, len(set.Vars))
		removedAny := false
		for _, variable := range set.Vars {
			if commonKeySet[variable.Key] {
				removedAny = true
				continue
			}
			filtered = append(filtered, variable)
		}

		if removedAny {
			affectedSets[setKey] = true
		}

		if len(filtered) == 0 {
			delete(m.Sets, setKey)
			continue
		}

		set.Vars = filtered
		m.Sets[setKey] = set
	}

	commonVars := make([]compose.Var, 0, len(commonKeys))
	for _, key := range commonKeys {
		commonVars = append(commonVars, varDefinitions[key])
	}

	if existing, ok := m.Sets["common"]; ok {
		existing.Description = "Shared environment for multiple services"
		existing.Vars = mergeVars(existing.Vars, commonVars)
		m.Sets["common"] = existing
	} else {
		m.Sets["common"] = compose.Set{
			Description: "Shared environment for multiple services",
			Vars:        commonVars,
		}
	}

	for index, service := range m.Services {
		updatedSets := make([]string, 0, len(service.Sets)+1)
		needsCommon := false
		for _, setKey := range service.Sets {
			if affectedSets[setKey] {
				needsCommon = true
			}
			if _, ok := m.Sets[setKey]; ok {
				updatedSets = append(updatedSets, setKey)
			}
		}
		if needsCommon {
			updatedSets = append([]string{"common"}, updatedSets...)
		}
		service.Sets = dedupePreserveOrder(updatedSets)
		m.Services[index] = service
	}
}

func sameManifestVar(left, right compose.Var) bool {
	return left.Key == right.Key &&
		left.Description == right.Description &&
		left.Default == right.Default &&
		left.Required == right.Required &&
		left.Secret == right.Secret &&
		left.Readonly == right.Readonly &&
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

func serviceSetKey(serviceName string) string {
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

func composeEnvToVars(env types.MappingWithEquals) []compose.Var {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	vars := make([]compose.Var, 0, len(keys))
	for _, key := range keys {
		var rawValue string
		if v := env[key]; v != nil {
			rawValue = *v
		} else {
			// KEY form: value comes from host env at runtime, no default.
			rawValue = "${" + key + "}"
		}
		defaultValue, editable, required := parseComposeEnvValue(key, rawValue)
		isSecret := isSecretVar(key)
		if isSecret {
			defaultValue = ""
		}

		vars = append(vars, compose.Var{
			Key:         key,
			Default:     defaultValue,
			Description: "Imported from compose environment",
			Required:    strconv.FormatBool(required),
			Secret:      strconv.FormatBool(isSecret),
			Readonly:    strconv.FormatBool(!editable),
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
