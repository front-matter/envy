package writer

import (
	"strings"
	"testing"

	"github.com/front-matter/envy/manifest"
)

func TestGenerateMarksRequiredWithoutOptionalMarker(t *testing.T) {
	m := &manifest.Manifest{
		Meta: manifest.Meta{Title: "Example"},
		Groups: map[string]manifest.Group{
			"app": {
				Vars: []manifest.Var{
					{Key: "REQ", Default: "x", Description: "required var", Required: "true", Editable: "true"},
					{Key: "OPT", Default: "y", Description: "optional var", Required: "false", Editable: "true"},
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

func TestGenerateSkipsNonEditableVars(t *testing.T) {
	m := &manifest.Manifest{
		Meta: manifest.Meta{Title: "Example"},
		Groups: map[string]manifest.Group{
			"app": {
				Vars: []manifest.Var{
					{Key: "EDITABLE_VAR", Default: "x", Description: "included", Editable: "true"},
					{Key: "LOCKED_VAR", Default: "y", Description: "excluded", Editable: "false"},
				},
			},
		},
	}

	output := Generate(m, Options{IncludeSecrets: true})

	if !strings.Contains(output, "EDITABLE_VAR=x") {
		t.Fatalf("expected editable variable in output, got:\n%s", output)
	}
	if strings.Contains(output, "LOCKED_VAR=y") {
		t.Fatalf("did not expect non-editable variable in output, got:\n%s", output)
	}
}
