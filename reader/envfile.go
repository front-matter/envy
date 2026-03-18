package reader

import (
	"fmt"
	"sort"

	"github.com/front-matter/envy/envfile"
	"github.com/front-matter/envy/manifest"
)

// ImportEnvFile reads a .env file and converts it to an envy manifest.
func ImportEnvFile(path string) (*manifest.Manifest, error) {
	env, err := envfile.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading env file %s: %w", path, err)
	}

	if len(env.Keys) == 0 {
		return nil, fmt.Errorf("env file %s has no variables", path)
	}

	// All .env variables go into a single "env" group
	vars := make([]manifest.Var, 0, len(env.Keys))

	for _, key := range env.Keys {
		value := env.Values[key]

		vars = append(vars, manifest.Var{
			Key:         key,
			Default:     value,
			Description: "Imported from .env file",
			Secret:      "true",
		})
	}

	sort.Slice(vars, func(i, j int) bool {
		return vars[i].Key < vars[j].Key
	})

	m := &manifest.Manifest{
		Meta: manifest.Meta{
			Name:        "Imported Env Manifest",
			Description: fmt.Sprintf("Generated from %s", path),
			Version:     "v1",
		},
		Groups: map[string]manifest.Group{
			"env": {
				Description: "Imported from .env file",
				Vars:        vars,
			},
		},
	}

	return m, nil
}
