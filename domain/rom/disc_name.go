package rom

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// DiscName is a filename stem split into its disc number and the rest of the name.
type DiscName struct {
	Number int
	// Base is the stem without the disc marker, whitespace collapsed.
	Base string
}

// discMarkers are tried in order. CD and DVD need a tag or a dash because "Sonic Sega CD 2" is a
// title, not a second disc.
var discMarkers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[(\[]\s*(?:disc|disk|cd|dvd)\s*-?\s*(\d{1,2})(?:\s+of\s+\d{1,2})?\s*[)\]]`),
	regexp.MustCompile(`(?i)(?:\s*[-_.]\s*|\s+)(?:disc|disk)\s*[-_.]?\s*(\d{1,2})$`),
	regexp.MustCompile(`(?i)\s*[-_]\s*(?:cd|dvd)\s*[-_.]?\s*(\d{1,2})$`),
}

// ParseDiscName finds a disc marker in a filename stem (the filename without its extension).
// It reports false when there is none, the number is 0, or nothing else is left of the name.
func ParseDiscName(stem string) (DiscName, bool) {
	for _, marker := range discMarkers {
		match := marker.FindStringSubmatchIndex(stem)
		if match == nil {
			continue
		}
		number, err := strconv.Atoi(stem[match[2]:match[3]])
		base := collapseSpace(stem[:match[0]] + " " + stem[match[1]:])
		if err != nil || number == 0 || base == "" {
			return DiscName{}, false
		}
		return DiscName{Number: number, Base: base}, true
	}
	return DiscName{}, false
}

// DiscGroupKey identifies a release's discs: the platform plus the name without its disc marker,
// lowercased with whitespace collapsed.
func DiscGroupKey(platform, base string) string {
	sum := sha256.Sum256([]byte(platform + "|" + strings.ToLower(collapseSpace(base))))
	return hex.EncodeToString(sum[:])
}

func collapseSpace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
