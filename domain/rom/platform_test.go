package rom

import "testing"

func TestPlatformForExtension(t *testing.T) {
	for extension, want := range map[string]string{
		"nes": "nes", ".SFC": "snes", "sms": "sms", "gg": "gg", "md": "genesis", "vpk": "vita",
		"iso": "psx", "chd": "psx", "bin": PlatformUnknown, "zip": PlatformUnknown, "": PlatformUnknown,
	} {
		if got := PlatformForExtension(extension); got != want {
			t.Errorf("PlatformForExtension(%q) = %q, want %q", extension, got, want)
		}
	}
}

func TestPlatformKeyForNameResolvesSourceNamesAndKeepsUnknownOnes(t *testing.T) {
	for name, want := range map[string]string{
		"Sega Master System":    "sms",
		"Sony PlayStation Vita": "vita",
		"PS Vita":               "vita",
		"Nintendo Super Nintendo Entertainment System": "snes",
		"TurboGrafx 16":                 "pce",
		"turbografx-16/pc engine":       "pce",
		"Nintendo Entertainment System": "nes",
		"Sony PlayStation":              "psx",
		"  Sega   Pico ":                "Sega Pico",
	} {
		if got := PlatformKeyForName(name); got != want {
			t.Errorf("PlatformKeyForName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestPlatformName(t *testing.T) {
	for platform, want := range map[string]string{
		"sms": "Master System", "VITA": "PlayStation Vita", "nes": "NES", "genesis": "Mega Drive / Genesis",
		"Sega Pico": "Sega Pico", "unknown": "Unknown", "": "Unknown",
	} {
		if got := PlatformName(platform); got != want {
			t.Errorf("PlatformName(%q) = %q, want %q", platform, got, want)
		}
	}
}

func TestPlatformCatalogHasNoDuplicateKeysExtensionsOrNames(t *testing.T) {
	keys, extensions, names := map[string]bool{}, map[string]string{}, map[string]string{}
	for _, platform := range Platforms() {
		if keys[platform.Key] {
			t.Errorf("duplicate key %q", platform.Key)
		}
		keys[platform.Key] = true
		for _, extension := range platform.Extensions {
			if other, ok := extensions[extension]; ok {
				t.Errorf("extension %q claimed by %q and %q", extension, other, platform.Key)
			}
			extensions[extension] = platform.Key
		}
		for _, name := range append([]string{platform.Key, platform.Name}, platform.Aliases...) {
			normalized := normalizePlatformName(name)
			if other, ok := names[normalized]; ok && other != platform.Key {
				t.Errorf("name %q claimed by %q and %q", name, other, platform.Key)
			}
			names[normalized] = platform.Key
		}
	}
}
