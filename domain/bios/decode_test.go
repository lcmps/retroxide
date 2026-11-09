package bios

import "testing"

func TestDecode(t *testing.T) {
	const none = "-"
	cases := []struct {
		platform, path                       string
		region, version, model, date, note string
	}{
		{"ps2", "pcsx2/bios/ps2-0160e-20010704.bin", "Europe", "1.60", none, "2001-07-04", none},
		{"ps2", "pcsx2/bios/ps2-0100j-20000117.bin", "Japan", "1.00", none, "2000-01-17", none},
		{"ps2", "pcsx2/bios/ps2-0170a-20030325.bin", "USA", "1.70", none, "2003-03-25", none},
		{"ps2", "pcsx2/bios/ps2-0120ed-20000902-20030110.bin", "Europe", "1.20", none, "2000-09-02", "Debug"},
		{"ps2", "pcsx2/bios/ps2-0101xd-20000224.bin", none, "1.01", none, "2000-02-24", "Debug"},
		{"ps2", "pcsx2/resources/unexpected.bin", none, none, none, none, none},
		{"psx", "scph5501.bin", "USA", none, "SCPH-5501", none, none},
		{"psx", "scph5502.bin", "Europe", none, "SCPH-5502", none, none},
		{"psx", "scph5500.bin", "Japan", none, "SCPH-5500", none, none},
		{"psx", "scph7003.bin", "Japan", none, "SCPH-7003", none, none},
		{"psx", "scph102A.bin", "Europe", none, "SCPH-102A", none, none},
		{"psx", "scph9002(7502).bin", "Europe", none, "SCPH-9002", none, none},
		{"psx", "ps1_rom.bin", none, none, none, none, none},
		{"segacd", "bios_CD_U.bin", "USA", none, none, none, none},
		{"segacd", "bios_CD_E.bin", "Europe", none, none, none, none},
		{"segacd", "bios_CD_J.bin", "Japan", none, none, none, none},
		{"pcecd", "syscard3.pce", "Japan", "3.0", none, none, none},
		{"pcecd", "syscard3u.pce", "USA", "3.0", none, none, none},
		{"pcecd", "syscard1.pce", "Japan", "1.0", none, none, none},
		{"pcecd", "gexpress.pce", none, none, none, none, none},
		{"saturn", "sega_101.bin", "Japan", "1.01", none, none, none},
		{"saturn", "mpr-17933.bin", "USA/Europe", "1.00", none, none, none},
		{"saturn", "hisaturn.bin", none, none, "Hitachi HiSaturn", none, none},
		{"saturn", "saturn_bios.bin", none, none, none, none, none},
		{"dreamcast", "dc/dc_boot.bin", none, none, none, none, "Boot ROM"},
		{"dreamcast", "dc/flash.bin", none, none, none, none, "Flash ROM"},
		{"nds", "bios7.bin", none, none, none, none, "ARM7 BIOS"},
		{"nds", "firmware.bin", none, none, none, none, "Firmware"},
		{"gba", "gba_bios.bin", none, none, none, none, none},
		{"fds", "disksys.rom", none, none, none, none, none},
		{"snes", "anything.bin", none, none, none, none, none},
	}
	for _, test := range cases {
		labels := Decode(test.platform, test.path)
		for _, field := range []struct {
			name string
			got  *string
			want string
		}{
			{"region", labels.Region, test.region},
			{"version", labels.Version, test.version},
			{"model", labels.Model, test.model},
			{"date", labels.Date, test.date},
			{"note", labels.Note, test.note},
		} {
			switch {
			case field.want == none && field.got != nil:
				t.Errorf("Decode(%q, %q) %s = %q, want none", test.platform, test.path, field.name, *field.got)
			case field.want != none && (field.got == nil || *field.got != field.want):
				t.Errorf("Decode(%q, %q) %s = %v, want %q", test.platform, test.path, field.name, deref(field.got), field.want)
			}
		}
	}
}

func deref(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
