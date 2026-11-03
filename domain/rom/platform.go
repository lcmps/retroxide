package rom

import (
	"strings"
	"unicode"
)

// PlatformUnknown marks a ROM whose platform could not be determined.
const PlatformUnknown = "unknown"

// Platform describes a system ROMs can belong to. Key is what ROMs store; Name is for display.
type Platform struct {
	Key  string
	Name string
	// Extensions are file extensions (lowercase, without the dot) that identify the platform
	// unambiguously.
	Extensions []string
	// Aliases are other names the platform goes by, such as those used by metadata sources.
	Aliases []string
}

// platformCatalog lists the platforms known by name. ROMs may still carry other platforms: a
// metadata source can report a system missing here, in which case its name is stored as-is.
var platformCatalog = []Platform{
	{Key: "nes", Name: "NES", Extensions: []string{"nes"}, Aliases: []string{"Nintendo Entertainment System", "Famicom", "Nintendo Famicom"}},
	{Key: "fds", Name: "Famicom Disk System", Extensions: []string{"fds"}, Aliases: []string{"Nintendo Famicom Disk System", "Family Computer Disk System"}},
	{Key: "snes", Name: "SNES", Extensions: []string{"sfc", "smc"}, Aliases: []string{"Super Nintendo Entertainment System", "Nintendo Super Nintendo Entertainment System", "Super Famicom", "Nintendo Super Famicom"}},
	{Key: "gb", Name: "Game Boy", Extensions: []string{"gb"}, Aliases: []string{"Nintendo Game Boy"}},
	{Key: "gbc", Name: "Game Boy Color", Extensions: []string{"gbc"}, Aliases: []string{"Nintendo Game Boy Color"}},
	{Key: "gba", Name: "Game Boy Advance", Extensions: []string{"gba"}, Aliases: []string{"GBA", "Nintendo Game Boy Advance"}},
	{Key: "n64", Name: "Nintendo 64", Extensions: []string{"n64", "z64", "v64"}},
	{Key: "nds", Name: "Nintendo DS", Extensions: []string{"nds"}, Aliases: []string{"Nintendo DSi"}},
	{Key: "3ds", Name: "Nintendo 3DS", Extensions: []string{"3ds", "cia"}, Aliases: []string{"Nintendo New 3DS"}},
	{Key: "vb", Name: "Virtual Boy", Extensions: []string{"vb"}, Aliases: []string{"Nintendo Virtual Boy"}},
	{Key: "pokemini", Name: "Pokémon Mini", Extensions: []string{"min"}, Aliases: []string{"Nintendo Pokemon Mini", "Pokemon Mini"}},
	{Key: "gc", Name: "GameCube", Extensions: []string{"gcm", "rvz"}, Aliases: []string{"Nintendo GameCube"}},
	{Key: "wii", Name: "Wii", Extensions: []string{"wbfs"}, Aliases: []string{"Nintendo Wii"}},
	{Key: "wiiu", Name: "Wii U", Extensions: []string{"wua"}, Aliases: []string{"Nintendo WiiU", "Nintendo Wii U"}},
	{Key: "switch", Name: "Nintendo Switch", Extensions: []string{"nsp", "xci"}},
	{Key: "sg1000", Name: "SG-1000", Extensions: []string{"sg"}, Aliases: []string{"Sega SG-1000"}},
	{Key: "sms", Name: "Master System", Extensions: []string{"sms"}, Aliases: []string{"Sega Master System", "Mark III", "Sega Mark III"}},
	{Key: "gg", Name: "Game Gear", Extensions: []string{"gg"}, Aliases: []string{"Sega Game Gear"}},
	{Key: "genesis", Name: "Mega Drive / Genesis", Extensions: []string{"md", "gen", "smd"}, Aliases: []string{"Sega Mega Drive / Genesis", "Sega Genesis", "Sega Mega Drive", "Mega Drive", "Genesis"}},
	{Key: "32x", Name: "Sega 32X", Extensions: []string{"32x"}, Aliases: []string{"32X"}},
	{Key: "segacd", Name: "Sega CD", Aliases: []string{"Sega Mega CD / Sega CD", "Sega Mega CD", "Mega CD"}},
	{Key: "saturn", Name: "Saturn", Aliases: []string{"Sega Saturn"}},
	{Key: "dreamcast", Name: "Dreamcast", Extensions: []string{"gdi", "cdi"}, Aliases: []string{"Sega Dreamcast"}},
	{Key: "psx", Name: "PlayStation", Aliases: []string{"Sony PlayStation", "PS1", "PSX"}},
	{Key: "ps2", Name: "PlayStation 2", Aliases: []string{"Sony PlayStation 2", "PS2"}},
	{Key: "ps3", Name: "PlayStation 3", Aliases: []string{"Sony PlayStation 3", "PS3"}},
	{Key: "psp", Name: "PlayStation Portable", Extensions: []string{"cso", "pbp"}, Aliases: []string{"Sony PlayStation Portable", "PSP"}},
	{Key: "vita", Name: "PlayStation Vita", Extensions: []string{"vpk"}, Aliases: []string{"Sony PlayStation Vita", "PS Vita"}},
	{Key: "pce", Name: "PC Engine / TurboGrafx-16", Extensions: []string{"pce"}, Aliases: []string{"TurboGrafx-16/PC Engine", "TurboGrafx 16", "TurboGrafx-16", "PC Engine", "NEC PC Engine"}},
	{Key: "pcecd", Name: "PC Engine CD", Aliases: []string{"Turbografx-16/PC Engine CD", "NEC PC-Engine CD & TurboGrafx-16 CD", "TurboGrafx-CD"}},
	{Key: "ngp", Name: "Neo Geo Pocket", Extensions: []string{"ngp"}},
	{Key: "ngpc", Name: "Neo Geo Pocket Color", Extensions: []string{"ngc"}},
	{Key: "ws", Name: "WonderSwan", Extensions: []string{"ws"}, Aliases: []string{"Bandai WonderSwan"}},
	{Key: "wsc", Name: "WonderSwan Color", Extensions: []string{"wsc"}, Aliases: []string{"Bandai WonderSwan Color"}},
	{Key: "a2600", Name: "Atari 2600", Extensions: []string{"a26"}},
	{Key: "a7800", Name: "Atari 7800", Extensions: []string{"a78"}},
	{Key: "lynx", Name: "Atari Lynx", Extensions: []string{"lnx"}},
	{Key: "jaguar", Name: "Atari Jaguar", Extensions: []string{"j64"}},
	{Key: "coleco", Name: "ColecoVision", Extensions: []string{"col"}},
	{Key: "intv", Name: "Intellivision", Extensions: []string{"int"}, Aliases: []string{"Mattel Intellivision"}},
	{Key: "vectrex", Name: "Vectrex", Extensions: []string{"vec"}},
	{Key: "3do", Name: "3DO", Aliases: []string{"3DO Interactive Multiplayer"}},
	{Key: "xbox", Name: "Xbox", Aliases: []string{"Microsoft Xbox"}},
}

// discImageExtensions are disc formats shared by many systems. They are attributed to the
// original PlayStation, the most common case, until a scrape reports the actual platform.
var discImageExtensions = []string{"iso", "cue", "chd"}

var (
	platformsByKey       = make(map[string]Platform, len(platformCatalog))
	platformsByExtension = make(map[string]Platform)
	platformsByName      = make(map[string]Platform)
)

func init() {
	for _, platform := range platformCatalog {
		platformsByKey[platform.Key] = platform
		for _, extension := range platform.Extensions {
			platformsByExtension[extension] = platform
		}
		for _, name := range append([]string{platform.Key, platform.Name}, platform.Aliases...) {
			platformsByName[normalizePlatformName(name)] = platform
		}
	}
	for _, extension := range discImageExtensions {
		platformsByExtension[extension] = platformsByKey["psx"]
	}
}

// Platforms returns the catalog of known platforms.
func Platforms() []Platform {
	return append([]Platform(nil), platformCatalog...)
}

// PlatformByKey looks up a catalog platform by its key, case-insensitively.
func PlatformByKey(key string) (Platform, bool) {
	platform, ok := platformsByKey[strings.ToLower(strings.TrimSpace(key))]
	return platform, ok
}

// PlatformExtensions returns every extension that identifies a known platform, including the
// shared disc image formats.
func PlatformExtensions() []string {
	extensions := make([]string, 0, len(platformsByExtension))
	for _, platform := range platformCatalog {
		extensions = append(extensions, platform.Extensions...)
	}
	return append(extensions, discImageExtensions...)
}

// PlatformForExtension returns the platform key for a file extension, or PlatformUnknown.
func PlatformForExtension(extension string) string {
	if platform, ok := platformsByExtension[strings.ToLower(strings.TrimPrefix(extension, "."))]; ok {
		return platform.Key
	}
	return PlatformUnknown
}

// PlatformKeyForName resolves a platform name, such as one reported by a metadata source, to its
// catalog key. Names missing from the catalog are returned trimmed, to be stored as-is.
func PlatformKeyForName(name string) string {
	if platform, ok := platformsByName[normalizePlatformName(name)]; ok {
		return platform.Key
	}
	return strings.Join(strings.Fields(name), " ")
}

// PlatformName returns the display name of a stored platform value. Catalog keys get their
// proper name; other values were stored from a metadata source's name and are shown as-is.
func PlatformName(platform string) string {
	trimmed := strings.TrimSpace(platform)
	if known, ok := platformsByKey[strings.ToLower(trimmed)]; ok {
		return known.Name
	}
	if trimmed == "" || strings.EqualFold(trimmed, PlatformUnknown) {
		return "Unknown"
	}
	return trimmed
}

// IsUnknownPlatform reports whether a stored platform value carries no platform information.
func IsUnknownPlatform(platform string) bool {
	trimmed := strings.TrimSpace(platform)
	return trimmed == "" || strings.EqualFold(trimmed, PlatformUnknown)
}

// normalizePlatformName folds case and drops punctuation and spacing, so "TurboGrafx 16" and
// "turbografx-16" compare equal.
func normalizePlatformName(name string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
