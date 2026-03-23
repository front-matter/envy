package writer

import (
	"strings"
	"testing"

	"github.com/front-matter/envy/compose"
)

func newWriterTestSet(vars []compose.Var) compose.Set {
	set := compose.NewSet()
	set.SetVars(vars)
	return set
}

func TestGenerateRendersVariableDescriptions(t *testing.T) {
	m := &compose.Project{
		Meta: compose.Meta{Title: "Example"},
		Sets: map[string]compose.Set{
			"app": newWriterTestSet([]compose.Var{{Key: "REQ", Default: "x", Description: "first var"}, {Key: "OPT", Default: "y", Description: "optional var"}}),
		},
	}

	output := Generate(m)

	if strings.Contains(output, "[REQUIRED]") {
		t.Fatalf("did not expect required marker, got:\n%s", output)
	}
	if !strings.Contains(output, "# first var") {
		t.Fatalf("expected first variable comment, got:\n%s", output)
	}
	if !strings.Contains(output, "# optional var") {
		t.Fatalf("expected optional variable comment without marker, got:\n%s", output)
	}
}

func TestGenerateIncludesAllVars(t *testing.T) {
	m := &compose.Project{
		Meta: compose.Meta{Title: "Example"},
		Sets: map[string]compose.Set{
			"app": newWriterTestSet([]compose.Var{{Key: "EDITABLE_VAR", Default: "x", Description: "included"}, {Key: "SECOND_VAR", Default: "y", Description: "excluded"}}),
		},
	}

	output := Generate(m)

	if !strings.Contains(output, "EDITABLE_VAR=x") {
		t.Fatalf("expected first variable in output, got:\n%s", output)
	}
	if !strings.Contains(output, "SECOND_VAR=y") {
		t.Fatalf("expected second variable in output, got:\n%s", output)
	}
}
