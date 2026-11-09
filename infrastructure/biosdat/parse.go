// Package biosdat reads libretro's System.dat, the list of known BIOS and firmware files, which
// is bundled unmodified (CC-BY-SA-4.0, credited in NOTICE) and refreshed from upstream.
package biosdat

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	biosdomain "github.com/lcmps/retroxide/domain/bios"
)

//go:embed System.dat
var embedded []byte

// sectionPlatforms maps System.dat's per-system sections to catalog platform keys. Only systems
// in Retroxide's platform catalog are mapped; the rest of the list is ignored.
var sectionPlatforms = map[string]string{
	"3DO Company, The - 3DO":                         "3do",
	"Atari - 7800":                                   "a7800",
	"Atari - Lynx":                                   "lynx",
	"Coleco - ColecoVision":                          "coleco",
	"Mattel - Intellivision":                         "intv",
	"NEC - PC Engine - TurboGrafx 16 - SuperGrafx":   "pcecd",
	"Nintendo - Famicom Disk System":                 "fds",
	"Nintendo - Gameboy":                             "gb",
	"Nintendo - Gameboy Color":                       "gbc",
	"Nintendo - Game Boy Advance":                    "gba",
	"Nintendo - GameCube":                            "gc",
	"Nintendo - Nintendo 64DD":                       "n64",
	"Nintendo - Nintendo DS":                         "nds",
	"Nintendo - Nintendo DSi":                        "nds",
	"Nintendo - Nintendo Entertainment System":       "nes",
	"Nintendo - Pokemon Mini":                        "pokemini",
	"Nintendo - Satellaview":                         "snes",
	"Nintendo - SuFami Turbo":                        "snes",
	"Nintendo - Super Game Boy":                      "snes",
	"Nintendo - Super Nintendo Entertainment System": "snes",
	"Sega - Dreamcast":                               "dreamcast",
	"Sega - Game Gear":                               "gg",
	"Sega - Master System - Mark III":                "sms",
	"Sega - Mega CD - Sega CD":                       "segacd",
	"Sega - Mega Drive - Genesis":                    "genesis",
	"Sega - Saturn":                                  "saturn",
	"Sony - PlayStation":                             "psx",
	"Sony - PlayStation 2":                           "ps2",
	"Sony - PlayStation Portable":                    "psp",
}

// supportExtensions mark files the list carries for emulators that are data, not firmware.
var supportExtensions = map[string]bool{
	"yaml": true, "yml": true, "json": true, "txt": true, "ini": true, "cfg": true, "xml": true,
	"ttf": true, "otf": true, "png": true, "jpg": true, "zim": true,
}

var (
	romName = regexp.MustCompile(`\bname ("(?:[^"\\]|\\.)*"|\S+)`)
	romSize = regexp.MustCompile(`\bsize (\d+)`)
	romSHA1 = regexp.MustCompile(`\bsha1 ([0-9A-Fa-f]{40})`)
)

// Parse reads a System.dat and returns its BIOS files on catalog platforms, labelled by
// biosdomain.Decode. A list with no such files is an error: a truncated download or an error
// page must never replace a working list.
func Parse(reader io.Reader) ([]biosdomain.Known, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var entries []biosdomain.Known
	platform := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "comment "):
			platform = sectionPlatforms[unquote(strings.TrimPrefix(line, "comment "))]
		case strings.HasPrefix(line, "rom (") && platform != "":
			if entry, ok := parseROM(line, platform); ok {
				entries = append(entries, entry)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading the BIOS list: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("the BIOS list has no BIOS files for known platforms")
	}
	return entries, nil
}

func parseROM(line, platform string) (biosdomain.Known, bool) {
	name, size, sha1 := romName.FindStringSubmatch(line), romSize.FindStringSubmatch(line), romSHA1.FindStringSubmatch(line)
	if name == nil || size == nil || sha1 == nil {
		return biosdomain.Known{}, false
	}
	filePath := unquote(name[1])
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(filePath), "."))
	if supportExtensions[extension] || strings.Contains(filePath, "/resources/") {
		return biosdomain.Known{}, false
	}
	bytes, err := strconv.ParseUint(size[1], 10, 64)
	if err != nil {
		return biosdomain.Known{}, false
	}
	return biosdomain.Known{
		Platform: platform,
		Path:     filePath,
		Size:     bytes,
		SHA1:     strings.ToLower(sha1[1]),
		Labels:   biosdomain.Decode(platform, filePath),
	}, true
}

func unquote(raw string) string {
	if value, err := strconv.Unquote(raw); err == nil {
		return value
	}
	return strings.Trim(raw, `"`)
}
