package rom

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	romdomain "github.com/lcmps/retroxide/domain/rom"
)

const (
	// maxDownloadNameBytes keeps the base name well within common filesystem limits (255 bytes)
	// once the extension is appended.
	maxDownloadNameBytes = 200
	maxExtensionBytes    = 16
)

// platformExtensions supplies a conventional extension when the uploaded filename is unknown.
var platformExtensions = map[string]string{
	"nes":  ".nes",
	"snes": ".sfc",
	"gba":  ".gba",
	"gb":   ".gb",
	"gbc":  ".gbc",
	"n64":  ".z64",
	"psx":  ".iso",
}

// downloadFilename names a ROM download after its title, keeping the extension of the file that
// was originally uploaded so emulators recognize the format. Without a usable title it falls back
// to the uploaded name, then to the ROM ID.
func downloadFilename(rom romdomain.ROM, uploadedFilename *string) string {
	original := ""
	if uploadedFilename != nil {
		original = path.Base(strings.ReplaceAll(*uploadedFilename, "\\", "/"))
	}
	extension := fileExtension(original)
	if extension == "" {
		extension = platformExtensions[strings.ToLower(rom.Identity.Platform)]
	}
	if extension == "" {
		extension = ".rom"
	}

	name := sanitizeFilename(rom.Identity.Title)
	if name == "" {
		name = sanitizeFilename(strings.TrimSuffix(original, path.Ext(original)))
	}
	if name == "" {
		name = sanitizeFilename(rom.ID)
	}
	if name == "" {
		name = "rom"
	}
	return name + extension
}

// fileExtension returns filename's lowercase extension (with the dot) when it looks like a real
// file extension, or "" otherwise.
func fileExtension(filename string) string {
	extension := strings.ToLower(path.Ext(filename))
	if len(extension) < 2 || len(extension) > maxExtensionBytes {
		return ""
	}
	for _, r := range extension[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return extension
}

// sanitizeFilename makes name safe as a file name on common desktop filesystems: path separators,
// characters Windows reserves, and control characters become spaces; whitespace is collapsed;
// leading and trailing dots and spaces are trimmed; and the result is capped at
// maxDownloadNameBytes without splitting a UTF-8 sequence.
func sanitizeFilename(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return ' '
		}
		return r
	}, name)
	cleaned = strings.Trim(strings.Join(strings.Fields(cleaned), " "), ". ")
	if len(cleaned) > maxDownloadNameBytes {
		cut := maxDownloadNameBytes
		for cut > 0 && !utf8.RuneStart(cleaned[cut]) {
			cut--
		}
		cleaned = strings.TrimRight(cleaned[:cut], ". ")
	}
	return cleaned
}
