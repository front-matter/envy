package reader

import (
	"fmt"
	"io"

	types "github.com/compose-spec/compose-go/v2/types"

	"github.com/front-matter/envy/compose"
	"github.com/front-matter/envy/envfile"
)

// ImportEnvFile reads a .env file and converts it to an envy compose.
func ImportEnvFile(path string) (*compose.Project, error) {
	env, err := envfile.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading env file %s: %w", path, err)
	}

	return projectFromEnv(env, path)
}

// ImportEnv reads .env content from r and converts it to an envy compose.
// source names the origin (e.g. a URL) and is used in messages and metadata.
func ImportEnv(r io.Reader, source string) (*compose.Project, error) {
	env, err := envfile.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parsing env content from %s: %w", source, err)
	}

	return projectFromEnv(env, source)
}

func projectFromEnv(env *envfile.Env, path string) (*compose.Project, error) {
	if len(env.Keys) == 0 {
		return nil, fmt.Errorf("env file %s has no variables", path)
	}

	// All .env variables go into a single "env" set.
	vars := types.MappingWithEquals{}

	for _, key := range env.Keys {
		value := env.Values[key]
		vars[key] = &value
	}

	sortedVars := types.MappingWithEquals{}
	for _, key := range env.Keys {
		sortedVars[key] = vars[key]
	}

	m := &compose.Project{
		Meta: compose.Meta{
			Title:        "Imported Env Manifest",
			Description:  fmt.Sprintf("Generated from %s", path),
			LanguageCode: "en-US",
			Version:      "v1",
		},
		Sets: map[string]compose.Set{},
	}

	set := compose.NewSet()
	set.SetDescription("Imported from .env file")
	set.SetVars(sortedVars)
	m.Sets["env"] = set

	return m, nil
}
