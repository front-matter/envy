// Package validator checks env values against the manifest spec.
package validator

import (
	"fmt"

	"github.com/front-matter/envy/manifest"
)

// Error represents a single validation failure.
type Error struct {
	Level string // MISSING | TYPE | FORMAT | VALUE | LENGTH
	Key   string
	Msg   string
}

func (e Error) String() string {
	return fmt.Sprintf("[%-8s] %s%s", e.Level, e.Key, e.Msg)
}

// Validate checks a map of env values against the manifest spec.
// Returns a slice of errors; empty slice means valid.
func Validate(m *manifest.Manifest, env map[string]string) []Error {
	var errs []Error

	for _, group := range m.OrderedGroups() {
		for _, v := range group.Vars {
			value, present := env[v.Key]

			if v.Required && (!present || value == "") {
				errs = append(errs, Error{Level: "MISSING", Key: v.Key})
				continue
			}
			if !present || value == "" {
				continue
			}
		}
	}

	return errs
}
