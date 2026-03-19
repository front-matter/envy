package cmd

import "embed"

// docsFS holds the embedded Hugo site skeleton including the Hextra theme.
// At build time the docs/ directory (containing themes/hextra/) is compiled
// into the binary so that envy build works without network access.
//
//go:embed all:docs
var docsFS embed.FS
