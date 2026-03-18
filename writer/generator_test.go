package writer

import (
	"strings"
	"testing"

	"github.com/front-matter/envy/manifest"
)

func TestGenerateMarksRequiredWithoutOptionalMarker(t *testing.T) {
	m := &manifest.Manifest{
		Meta: manifest.Meta{Name: "Example"},
		Groups: map[string]manifest.Group{
			"app": {
				Vars: []manifest.Var{
					{Key: "REQ", Default: manifest.ScalarValue("x"), Description: "required var", Required: true},
					{Key: "OPT", Default: manifest.ScalarValue("y"), Description: "optional var", Required: false},
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
