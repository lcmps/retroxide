package rom

import "testing"

func TestParseDiscName(t *testing.T) {
	tests := []struct {
		stem   string
		number int
		base   string
	}{
		{"Final Fantasy VII (USA) (Disc 1)", 1, "Final Fantasy VII (USA)"},
		{"Onimusha - Dawn of Dreams (USA) (Disc 2)", 2, "Onimusha - Dawn of Dreams (USA)"},
		{"Game (Europe) (En,Fr) (Disc 2 of 3) (Rev 1)", 2, "Game (Europe) (En,Fr) (Rev 1)"},
		{"Game (CD 2)", 2, "Game"},
		{"Game [Disc 3]", 3, "Game"},
		{"Game (Disk 2)", 2, "Game"},
		{"Game (DVD 2)", 2, "Game"},
		{"Game (CD2)", 2, "Game"},
		{"Game - Disc 2", 2, "Game"},
		{"Game_disc2", 2, "Game"},
		{"Game Disc 2", 2, "Game"},
		{"Game - CD 2", 2, "Game"},
	}
	for _, test := range tests {
		t.Run(test.stem, func(t *testing.T) {
			got, ok := ParseDiscName(test.stem)
			if !ok || got.Number != test.number || got.Base != test.base {
				t.Fatalf("ParseDiscName(%q) = %+v, %v; want %d, %q", test.stem, got, ok, test.number, test.base)
			}
		})
	}
}

func TestParseDiscNameLeavesOtherNamesAlone(t *testing.T) {
	for _, stem := range []string{"Final Fantasy VII (USA)", "Sonic Sega CD 2", "Disc Station 2 (Japan)", "Game (Disc 0)", "Game (Disc 100)", "Disc 2", "(Disc 2)"} {
		if got, ok := ParseDiscName(stem); ok {
			t.Errorf("ParseDiscName(%q) = %+v, want no match", stem, got)
		}
	}
}

func TestDiscGroupKey(t *testing.T) {
	if DiscGroupKey("psx", "Game (USA)") != DiscGroupKey("psx", "game  (usa)") {
		t.Error("case and spacing must not change the key")
	}
	if DiscGroupKey("psx", "Game (USA)") == DiscGroupKey("psx", "Game (Japan)") {
		t.Error("another tag must change the key")
	}
	if DiscGroupKey("psx", "Game (USA)") == DiscGroupKey("ps2", "Game (USA)") {
		t.Error("another platform must change the key")
	}
}
