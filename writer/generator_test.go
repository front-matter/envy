package writer

import (
	"strings"
	"testing"

	"github.com/front-matter/envy/manifest"
)

func TestGenerateMarksRequiredWithoutOptionalMarker(t *testing.T) {
	m := &manifest.Manifest{
		Meta: manifest.Meta{Title: "Example"},
		Sets: map[string]manifest.Set{
			"app": {
				Vars: []manifest.Var{
					{Key: "REQ", Default: "x", Description: "required var", Required: "true"},
					{Key: "OPT", Default: "y", Description: "optional var", Required: "false"},
				},
			},
		},
	}

	output := Generate(m, Options{IncludeSecrets: true})

	if !strings.Contains(output, "# [REQUIRED] required var") {
		t.Fatalf("expected [REQUIRED] marker for required vars, got:\n%s", output)
	}
	if strings.Contains(output, "[optional]") {
		t.Fatalf("did not expect [optional] marker, got:\n%s", output)
	}
	if !strings.Contains(output, "# optional var") {
		t.Fatalf("expected optional variable comment without marker, got:\n%s", output)
	}
}

func TestGenerateSkipsReadonlyVars(t *testing.T) {
	m := &manifest.Manifest{
		Meta: manifest.Meta{Title: "Example"},
		Sets: map[string]manifest.Set{
			"app": {
				Vars: []manifest.Var{
					{Key: "EDITABLE_VAR", Default: "x", Description: "included"},
					{Key: "LOCKED_VAR", Default: "y", Description: "excluded", Readonly: "true"},
				},
			},
		},
	}

	output := Generate(m, Options{IncludeSecrets: true})

	if !strings.Contains(output, "EDITABLE_VAR=x") {
		t.Fatalf("expected non-readonly variable in output, got:\n%s", output)
	}
	if strings.Contains(output, "LOCKED_VAR=y") {
		t.Fatalf("did not expect readonly variable in output, got:\n%s", output)
	}
}
