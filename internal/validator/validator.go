// Package validator checks env values against the manifest spec.
package validator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/front-matter/envy/internal/manifest"
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

// urlPrefixes lists valid URL scheme prefixes for the "url" type.
var urlPrefixes = []string{
	"http://", "https://",
	"redis://", "rediss://",
	"postgresql", "postgres://",
	"s3://",
	"amqp://", "amqps://",
}

// Validate checks a map of env values against the manifest spec.
// Returns a slice of errors; empty slice means valid.
func Validate(m *manifest.Manifest, env map[string]string) []Error {
	var errs []Error

	for _, group := range m.Groups {
		for _, v := range group.Vars {
			value, present := env[v.Key]

			if v.Required && (!present || value == "") {
				errs = append(errs, Error{Level: "MISSING", Key: v.Key})
				continue
			}
			if !present || value == "" {
				continue
			}

			errs = append(errs, checkType(v, value)...)
			errs = append(errs, checkAllowed(v, value)...)
			errs = append(errs, checkLength(v, value)...)
		}
	}

	return errs
}

func checkType(v manifest.Var, value string) []Error {
	switch v.Type {
	case "bool":
		valid := map[string]bool{
			"true": true, "false": true,
			"True": true, "False": true,
			"1": true, "0": true,
		}
		if !valid[value] {
			return []Error{{
				Level: "TYPE",
				Key:   v.Key,
				Msg:   fmt.Sprintf(" = %q — expected bool (True/False/1/0)", value),
			}}
		}

	case "int":
		if _, err := strconv.Atoi(value); err != nil {
			return []Error{{
				Level: "TYPE",
				Key:   v.Key,
				Msg:   fmt.Sprintf(" = %q — expected integer", value),
			}}
		}

	case "url":
		for _, prefix := range urlPrefixes {
			if strings.HasPrefix(value, prefix) {
				return nil
			}
		}
		return []Error{{
			Level: "FORMAT",
			Key:   v.Key,
			Msg:   fmt.Sprintf(" = %q — expected URL", value),
		}}
	}

	return nil
}

func checkAllowed(v manifest.Var, value string) []Error {
	if len(v.Allowed) == 0 {
		return nil
	}
	for _, a := range v.Allowed {
		if value == a {
			return nil
		}
	}
	return []Error{{
		Level: "VALUE",
		Key:   v.Key,
		Msg: fmt.Sprintf(
			" = %q — must be one of: %s", value, strings.Join(v.Allowed, ", "),
		),
	}}
}

func checkLength(v manifest.Var, value string) []Error {
	if v.MinLength > 0 && len(value) < v.MinLength {
		return []Error{{
			Level: "LENGTH",
			Key:   v.Key,
			Msg:   fmt.Sprintf(" — min %d chars, got %d", v.MinLength, len(value)),
		}}
	}
	return nil
}
