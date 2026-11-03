// Package appearance holds presentation choices shared by the application settings and each
// user's preferences.
package appearance

// Interface fonts. Pixel is the default, Press Start 2P; System uses the operating system's font.
const (
	FontPixel  = "pixel"
	FontUbuntu = "ubuntu"
	FontSystem = "system"
)

// Fonts lists every interface font in display order.
func Fonts() []string {
	return []string{FontPixel, FontUbuntu, FontSystem}
}

// IsFont reports whether value names an interface font.
func IsFont(value string) bool {
	switch value {
	case FontPixel, FontUbuntu, FontSystem:
		return true
	default:
		return false
	}
}
