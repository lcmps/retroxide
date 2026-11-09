package bios

import (
	"path"
	"regexp"
	"strings"
)

// Decode reads the labels a known BIOS file's name encodes, for the platforms whose BIOS users
// most often need. Names on other platforms, or names a platform's convention doesn't cover,
// decode to empty labels: the list shows the filename instead.
func Decode(platform, filePath string) Labels {
	decoder, ok := decoders[platform]
	if !ok {
		return Labels{}
	}
	return decoder(strings.ToLower(path.Base(filePath)), path.Base(filePath))
}

// A decoder gets the file's base name in lower case, and as written.
type decoder func(lower, original string) Labels

var decoders = map[string]decoder{
	"ps2":       decodePS2,
	"psx":       decodePlayStation,
	"segacd":    decodeSegaCD,
	"pcecd":     decodePCEngineCD,
	"saturn":    decodeSaturn,
	"dreamcast": decodeByName(map[string]Labels{"dc_boot.bin": note("Boot ROM"), "boot.bin": note("Boot ROM"), "flash.bin": note("Flash ROM")}),
	"nds":       decodeByName(map[string]Labels{"bios7.bin": note("ARM7 BIOS"), "bios9.bin": note("ARM9 BIOS"), "firmware.bin": note("Firmware")}),
}

// ps2Name matches PCSX2's naming: ps2-<version><region>[d]-<yyyymmdd>[-…].bin, e.g.
// ps2-0160e-20010704.bin is version 1.60 for Europe, built 2001-07-04.
var ps2Name = regexp.MustCompile(`^ps2-(\d{2})(\d{2})([a-z])(d?)-(\d{4})(\d{2})(\d{2})`)

var ps2Regions = map[string]string{"j": "Japan", "a": "USA", "e": "Europe", "h": "Asia", "c": "China"}

func decodePS2(lower, _ string) Labels {
	match := ps2Name.FindStringSubmatch(lower)
	if match == nil {
		return Labels{}
	}
	labels := Labels{
		Version: text(strings.TrimPrefix(match[1], "0") + "." + match[2]),
		Date:    text(match[5] + "-" + match[6] + "-" + match[7]),
	}
	if region, ok := ps2Regions[match[3]]; ok {
		labels.Region = text(region)
	}
	if match[4] == "d" {
		labels.Note = text("Debug")
	}
	return labels
}

// playStationName matches scph<number>[letter], e.g. scph5501.bin or scph102A.bin.
var playStationName = regexp.MustCompile(`^scph(\d+)([a-z]?)`)

func decodePlayStation(lower, _ string) Labels {
	match := playStationName.FindStringSubmatch(lower)
	if match == nil {
		return Labels{}
	}
	number := match[1]
	labels := Labels{Model: text("SCPH-" + number + strings.ToUpper(match[2]))}
	// The model number's last digit is its region.
	switch number[len(number)-1] {
	case '0', '3':
		labels.Region = text("Japan")
	case '1':
		labels.Region = text("USA")
	case '2':
		labels.Region = text("Europe")
	}
	return labels
}

func decodeSegaCD(lower, _ string) Labels {
	regions := map[string]string{"bios_cd_e.bin": "Europe", "bios_cd_j.bin": "Japan", "bios_cd_u.bin": "USA"}
	if region, ok := regions[lower]; ok {
		return Labels{Region: text(region)}
	}
	return Labels{}
}

// pceSystemCard matches syscard<version>[u].pce: the System Card version, u for the US
// (TurboGrafx) card.
var pceSystemCard = regexp.MustCompile(`^syscard(\d)(u?)\.pce$`)

func decodePCEngineCD(lower, _ string) Labels {
	match := pceSystemCard.FindStringSubmatch(lower)
	if match == nil {
		return Labels{}
	}
	region := "Japan"
	if match[2] == "u" {
		region = "USA"
	}
	return Labels{Region: text(region), Version: text(match[1] + ".0")}
}

// saturnFiles lists only the Saturn BIOS files whose identity is certain; the rest show their
// filename.
var saturnFiles = map[string]Labels{
	"sega_100.bin":  {Region: text("Japan"), Version: text("1.00")},
	"sega_100a.bin": {Region: text("Japan"), Version: text("1.00a")},
	"sega_101.bin":  {Region: text("Japan"), Version: text("1.01")},
	"sega1003.bin":  {Region: text("Japan"), Version: text("1.003")},
	"mpr-17933.bin": {Region: text("USA/Europe"), Version: text("1.00")},
	"hisaturn.bin":  {Model: text("Hitachi HiSaturn")},
	"vsaturn.bin":   {Model: text("Victor V-Saturn")},
}

func decodeSaturn(lower, _ string) Labels {
	return saturnFiles[lower]
}

func decodeByName(files map[string]Labels) decoder {
	return func(lower, _ string) Labels {
		return files[lower]
	}
}

func note(value string) Labels {
	return Labels{Note: text(value)}
}

func text(value string) *string {
	return &value
}
