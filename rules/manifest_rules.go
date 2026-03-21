package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// WrappingRules defines error-wrapping format checks.
func WrappingRules(m dsl.Matcher) {
	// Prefer wrapped errors for propagation when contextualizing existing errors.
	m.Match(`fmt.Errorf($format, $err)`).Where(
		m.File().PkgPath.Matches(`^github.com/front-matter/envy($|/)`) && m["format"].Text.Matches(`".*%v.*"`),
	).
		Report(`use %w in fmt.Errorf when wrapping errors`)
}

// ManifestAPIRules defines checks around manifest lint API usage in CLI code.
func ManifestAPIRules(m dsl.Matcher) {
	// Commands should consume structured issues to preserve severity and rule IDs.
	m.Match(`$x.Lint()`).Where(m.File().PkgPath.Matches(`^github.com/front-matter/envy/cmd$`)).
		Report(`prefer LintIssues() over Lint() in cmd package to keep severity and rule metadata`)
}
