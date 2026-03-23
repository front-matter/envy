package writer

import (
	"strings"
	"testing"

	types "github.com/compose-spec/compose-go/v2/types"

	"github.com/front-matter/envy/compose"
)

func newWriterTestSet(vars types.MappingWithEquals) compose.Set {
	set := compose.NewSet()
	set.SetVars(vars)
	return set
}

func TestGenerateRendersVariableDescriptions(t *testing.T) {
	m := &compose.Project{
		Meta: compose.Meta{Title: "Example"},
		Sets: map[string]compose.Set{
			"app": newWriterTestSet(types.MappingWithEquals{"REQ": strPtr("x"), "OPT": strPtr("y")}),
		},
	}

	output := Generate(m)

	if strings.Contains(output, "[REQUIRED]") {
		t.Fatalf("did not expect required marker, got:\n%s", output)
	}
	if !strings.Contains(output, "REQ=x") {
		t.Fatalf("expected first variable output, got:\n%s", output)
	}
	if !strings.Contains(output, "OPT=y") {
		t.Fatalf("expected second variable output, got:\n%s", output)
	}
}

func TestGenerateIncludesAllVars(t *testing.T) {
	m := &compose.Project{
		Meta: compose.Meta{Title: "Example"},
		Sets: map[string]compose.Set{
			"app": newWriterTestSet(types.MappingWithEquals{"EDITABLE_VAR": strPtr("x"), "SECOND_VAR": strPtr("y")}),
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

func strPtr(value string) *string {
	v := value
	return &v
}
