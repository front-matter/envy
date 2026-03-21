package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// ErrorRules defines error-construction checks.
func ErrorRules(m dsl.Matcher) {
	// Avoid wrapping formatted strings inside errors.New.
	m.Match(`errors.New(fmt.Sprintf($format, $*args))`).Where(m.File().PkgPath.Matches(`^github.com/front-matter/envy($|/)`)).
		Report(`use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))`)
}

// PanicRules defines panic-avoidance checks for application code.
func PanicRules(m dsl.Matcher) {
	// Prefer returning errors over panics in normal control flow.
	m.Match(`panic($msg)`).Where(m.File().PkgPath.Matches(`^github.com/front-matter/envy($|/)`)).
		Report(`avoid panic in application code; return an error instead`)
}
