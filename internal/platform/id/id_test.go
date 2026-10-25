package id

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	first, err := New("upl")
	if err != nil {
		t.Fatal(err)
	}
	second, err := New("upl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "upl_") || len(first) != len("upl_")+32 {
		t.Fatalf("New() = %q, want upl_ followed by 32 hex characters", first)
	}
	for _, char := range first[len("upl_"):] {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			t.Fatalf("id suffix contains non-lowercase-hex character %q", char)
		}
	}
	if first == second {
		t.Fatalf("two generated IDs are equal: %q", first)
	}
}
