package reader

import (
	"fmt"
	"sort"

	"github.com/front-matter/envy/compose"
	"github.com/front-matter/envy/envfile"
)

// ImportEnvFile reads a .env file and converts it to an envy compose.
func ImportEnvFile(path string) (*compose.Project, error) {
	env, err := envfile.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading env file %s: %w", path, err)
	}

	if len(env.Keys) == 0 {
		return nil, fmt.Errorf("env file %s has no variables", path)
	}

	// All .env variables go into a single "env" set.
	vars := make([]compose.Var, 0, len(env.Keys))

	for _, key := range env.Keys {
		vars = append(vars, compose.Var{
			Key:         key,
			Default:     "",
			Description: "Imported from .env file",
			Secret:      "true",
		})
	}

	sort.Slice(vars, func(i, j int) bool {
		return vars[i].Key < vars[j].Key
	})

	m := &compose.Project{
		Meta: compose.Meta{
			Title:        "Imported Env Manifest",
			Description:  fmt.Sprintf("Generated from %s", path),
			LanguageCode: "en-US",
			Version:      "v1",
		},
		Sets: map[string]compose.Set{
			"env": {
				Description: "Imported from .env file",
				Vars:        vars,
			},
		},
	}

	return m, nil
}
