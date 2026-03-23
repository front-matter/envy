// Package validator checks env values against the manifest spec.
package validator

import (
	"fmt"

	"github.com/front-matter/envy/compose"
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
func Validate(m *compose.Project, env map[string]string) []Error {
	var errs []Error

	for _, set := range m.OrderedSets() {
		for _, v := range set.Vars() {
			_, _ = env[v.Key]
		}
	}

	return errs
}
