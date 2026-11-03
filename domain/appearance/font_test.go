package appearance

import "testing"

func TestIsFontAcceptsExactlyTheListedFonts(t *testing.T) {
	for _, font := range Fonts() {
		if !IsFont(font) {
			t.Errorf("IsFont(%q) = false", font)
		}
	}
	for _, font := range []string{"", "Pixel", "comic-sans"} {
		if IsFont(font) {
			t.Errorf("IsFont(%q) = true", font)
		}
	}
}
