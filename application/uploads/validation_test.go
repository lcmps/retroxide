package uploads

import (
	"testing"

	"github.com/lcmps/retroxide/application/fault"
)

func TestValidateFilenameRefusesDotNames(t *testing.T) {
	for _, name := range []string{".", "..", "a/b", `a\b`, "", "a\x00b"} {
		if err := ValidateFilename(name); !fault.IsCode(err, fault.CodeValidation) {
			t.Errorf("ValidateFilename(%q) = %v, want a validation error", name, err)
		}
	}
	for _, name := range []string{"a..b.bin", "...bin", "A...B (Disc 1).cue", ".hidden"} {
		if err := ValidateFilename(name); err != nil {
			t.Errorf("ValidateFilename(%q) = %v", name, err)
		}
	}
}
