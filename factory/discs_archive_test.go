package factory

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/discs"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"github.com/lcmps/retroxide/infrastructure/discimage"
	romhash "github.com/lcmps/retroxide/infrastructure/hash"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

func sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

const dataSheet = "FILE \"track01.bin\" BINARY\n  TRACK 01 MODE1/2352\n    INDEX 01 00:00:00\n"

// dataDisc assembles a disc kept as tracks, a single data track named track01.bin, and returns
// the track's bytes.
func dataDisc(t *testing.T, service *discs.Service, files *storage.Filesystem, sheetName string) (romdomain.ROMFile, []byte) {
	t.Helper()
	tracks, _ := discTracks(t)
	data := tracks["Game (Track 1).bin"] // the fixture's MODE1/2352 track
	hash, _ := romhash.New().Hash(data)
	key, err := files.Write(context.Background(), hash.SHA256, data)
	if err != nil {
		t.Fatal(err)
	}
	romFile, err := service.Assemble(context.Background(), sheetName, dataSheet,
		[]discs.Part{{Name: "track01.bin", StoragePath: key, Size: int64(len(data)), Hash: hash}}, false)
	if err != nil {
		t.Fatal(err)
	}
	return romFile, data
}

// storedFile puts content in storage and returns the plain file that holds it.
func storedFile(t *testing.T, files *storage.Filesystem, content []byte, format romdomain.FileFormat) romdomain.ROMFile {
	t.Helper()
	hash, _ := romhash.New().Hash(content)
	key, err := files.Write(context.Background(), hash.SHA256, content)
	if err != nil {
		t.Fatal(err)
	}
	return romdomain.ROMFile{ID: "rf_" + hash.SHA256, Hash: hash, Size: uint64(len(content)), StoragePath: key, Format: format}
}

// threeDiscs is a title whose disc 1 is kept as tracks, disc 2 is a CHD and disc 3 an ISO. It
// returns the discs to archive and the SHA-1 of each file the zip must hold, by entry name.
func threeDiscs(t *testing.T, name func(number int) string) (*discs.Service, []romdomain.ArchiveDisc, map[string]string) {
	t.Helper()
	service, _, files := compressionFixture(t, nil)
	first, data := dataDisc(t, service, files, name(1)+".cue")
	chdContent, err := os.ReadFile(filepath.Join(chdFixtures, "chd", "cd-cdlz.chd"))
	if err != nil {
		t.Fatal(err)
	}
	iso := bytes.Repeat([]byte("ISO9660 sector of a plain image "), 2048*20/32)
	archived := []romdomain.ArchiveDisc{
		{ROMFile: first, Name: name(1), Filename: name(1) + ".cue", AsTracks: true},
		{ROMFile: storedFile(t, files, chdContent, romdomain.FormatCHD), Name: name(2), Filename: name(2) + ".chd"},
		{ROMFile: storedFile(t, files, iso, romdomain.FormatFile), Name: name(3), Filename: name(3) + ".iso"},
	}
	want := map[string]string{
		name(1) + "/" + name(1) + ".cue": sha1Hex([]byte(dataSheet)),
		name(1) + "/track01.bin":         sha1Hex(data),
		name(2) + ".chd":                 sha1Hex(chdContent),
		name(3) + ".iso":                 sha1Hex(iso),
	}
	return service, archived, want
}

// keepZip saves a zip as dir/name.zip when RETROXIDE_TEST_ZIP_DIR is set, to check it with native
// tools such as unzip, 7z and bsdtar.
func keepZip(t *testing.T, name string, archive []byte) {
	t.Helper()
	if dir := os.Getenv("RETROXIDE_TEST_ZIP_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, name+".zip"), archive, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func openZip(t *testing.T, archive []byte) *zip.Reader {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func zipEntry(t *testing.T, file *zip.File) []byte {
	t.Helper()
	entry, err := file.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	data, err := io.ReadAll(entry)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func gameDisc(number int) string { return "Game (Disc " + string(rune('0'+number)) + ")" }

func TestDownloadAllZip(t *testing.T) {
	service, archived, want := threeDiscs(t, gameDisc)
	var out bytes.Buffer
	if err := service.ArchiveDiscs(context.Background(), "Game", archived, &out); err != nil {
		t.Fatal(err)
	}
	keepZip(t, "all", out.Bytes())
	reader := openZip(t, out.Bytes())
	var names []string
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	wantNames := "Game (Disc 1)/Game (Disc 1).cue|Game (Disc 1)/track01.bin|Game (Disc 2).chd|Game (Disc 3).iso|Game.m3u"
	if strings.Join(names, "|") != wantNames {
		t.Fatalf("entries = %v, want %s", names, wantNames)
	}
	for _, file := range reader.File {
		if file.Modified.IsZero() {
			t.Errorf("%s has no modified time", file.Name)
		}
		if strings.Contains(file.Name, "..") || strings.Contains(file.Name, `\`) || strings.HasPrefix(file.Name, "/") {
			t.Errorf("unsafe entry name %q", file.Name)
		}
		wantMethod := zip.Deflate
		if strings.HasSuffix(file.Name, ".chd") {
			wantMethod = zip.Store
		}
		if file.Method != wantMethod {
			t.Errorf("%s method = %d, want %d", file.Name, file.Method, wantMethod)
		}
		if expected, ok := want[file.Name]; ok {
			if got := sha1Hex(zipEntry(t, file)); got != expected {
				t.Errorf("%s SHA-1 = %s, want the uploaded %s", file.Name, got, expected)
			}
		}
		if file.Name == "Game (Disc 1)/track01.bin" && file.CompressedSize64 >= file.UncompressedSize64 {
			t.Errorf("track01.bin compressed to %d of %d bytes", file.CompressedSize64, file.UncompressedSize64)
		}
	}
}

func TestDownloadAllPlaylist(t *testing.T) {
	service, archived, _ := threeDiscs(t, gameDisc)
	var out bytes.Buffer
	if err := service.ArchiveDiscs(context.Background(), "Game", archived, &out); err != nil {
		t.Fatal(err)
	}
	reader := openZip(t, out.Bytes())
	var playlist []byte
	entries := map[string]bool{}
	for _, file := range reader.File {
		entries[file.Name] = true
		if file.Name == "Game.m3u" {
			playlist = zipEntry(t, file)
		}
	}
	if want := "Game (Disc 1)/Game (Disc 1).cue\nGame (Disc 2).chd\nGame (Disc 3).iso\n"; string(playlist) != want {
		t.Fatalf("playlist = %q, want %q", playlist, want)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(playlist), "\n"), "\n") {
		if !entries[line] {
			t.Errorf("playlist line %q names no entry", line)
		}
	}

	t.Run("a CHD offered as tracks and a GDI disc list their sheets", func(t *testing.T) {
		service, _, files := compressionFixture(t, nil)
		chdContent, err := os.ReadFile(filepath.Join(chdFixtures, "chd", "cd-cdlz.chd"))
		if err != nil {
			t.Fatal(err)
		}
		sheet, parts := discFixture(t, files, "gd.chd", "Disc")
		gdi, err := service.Assemble(context.Background(), "Disc.gdi", sheet, parts, false)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		err = service.ArchiveDiscs(context.Background(), "Game", []romdomain.ArchiveDisc{
			{ROMFile: storedFile(t, files, chdContent, romdomain.FormatCHD), Name: "Game (Disc 1)", Filename: "Game (Disc 1).chd", AsTracks: true},
			{ROMFile: gdi, Name: "Game (Disc 2)", Filename: "Game (Disc 2).gdi", AsTracks: true},
		}, &out)
		if err != nil {
			t.Fatal(err)
		}
		reader := openZip(t, out.Bytes())
		last := reader.File[len(reader.File)-1]
		if want := "Game (Disc 1)/Game (Disc 1).cue\nGame (Disc 2)/Game (Disc 2).gdi\n"; last.Name != "Game.m3u" || string(zipEntry(t, last)) != want {
			t.Fatalf("%s = %q, want %q", last.Name, zipEntry(t, last), want)
		}
	})
}

func TestZipNamesSurviveNonASCIITitles(t *testing.T) {
	name := func(number int) string { return "Pokémon Ação (Disc " + string(rune('0'+number)) + ")" }
	service, archived, _ := threeDiscs(t, name)
	var out bytes.Buffer
	if err := service.ArchiveDiscs(context.Background(), "Pokémon Ação", archived, &out); err != nil {
		t.Fatal(err)
	}
	keepZip(t, "non-ascii", out.Bytes())
	reader := openZip(t, out.Bytes())
	var playlist string
	for _, file := range reader.File {
		if strings.Contains(file.Name, "é") && file.Flags&0x800 == 0 {
			t.Errorf("%q lacks the UTF-8 flag", file.Name)
		}
		if file.NonUTF8 {
			t.Errorf("%q is not read as UTF-8", file.Name)
		}
		if file.Name == "Pokémon Ação.m3u" {
			playlist = string(zipEntry(t, file))
		}
	}
	if want := "Pokémon Ação (Disc 1)/Pokémon Ação (Disc 1).cue\nPokémon Ação (Disc 2).chd\nPokémon Ação (Disc 3).iso\n"; playlist != want {
		t.Fatalf("playlist = %q, want %q", playlist, want)
	}
}

func TestSingleDiscZipHasNoPlaylist(t *testing.T) {
	ctx := context.Background()
	service, _, files := compressionFixture(t, nil)
	sheet, parts := discFixture(t, files, "cd-cdlz.chd", "Game")
	file, err := service.Assemble(ctx, "Game.cue", sheet, parts, false)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := service.Archive(ctx, file, "Game", &out); err != nil {
		t.Fatal(err)
	}
	reader := openZip(t, out.Bytes())
	if len(reader.File) != len(parts)+1 {
		t.Fatalf("%d entries, want the sheet and %d tracks", len(reader.File), len(parts))
	}
	want := map[string]string{"Game.cue": sha1Hex([]byte(sheet))}
	for _, part := range parts {
		want[part.Name] = part.Hash.SHA1
	}
	for _, entry := range reader.File {
		expected, ok := want[entry.Name]
		if !ok || strings.HasSuffix(entry.Name, ".m3u") {
			t.Errorf("unexpected entry %q", entry.Name)
			continue
		}
		if got := sha1Hex(zipEntry(t, entry)); got != expected {
			t.Errorf("%s SHA-1 = %s, want %s", entry.Name, got, expected)
		}
		if strings.HasSuffix(entry.Name, ".bin") && entry.Method != zip.Deflate {
			t.Errorf("%s method = %d, want Deflate", entry.Name, entry.Method)
		}
	}
}

// zeroReader reads zeros.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestDownloadAllOver4GiB(t *testing.T) {
	if testing.Short() {
		t.Skip("writes and reads a 4.5 GiB entry")
	}
	const size = 4*1024*1024*1024 + 512*1024*1024
	ctx := context.Background()
	_, repositories, _ := compressionFixture(t, nil)
	root := t.TempDir()
	service := discs.NewService(repositories, storage.New(root), romhash.New(), nil, nil)
	key := "00/" + strings.Repeat("0", 64)
	if err := os.MkdirAll(filepath.Join(root, "00"), 0o755); err != nil {
		t.Fatal(err)
	}
	sparse, err := os.Create(filepath.Join(root, filepath.FromSlash(key)))
	if err != nil {
		t.Fatal(err)
	}
	if err := sparse.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := sparse.Close(); err != nil {
		t.Fatal(err)
	}
	hash, err := romdomain.NewROMHash(strings.Repeat("0", 64), strings.Repeat("0", 40), strings.Repeat("0", 32), "00000000")
	if err != nil {
		t.Fatal(err)
	}
	big := romdomain.ROMFile{ID: "rf_big", Hash: hash, Size: size, StoragePath: key, Format: romdomain.FormatFile}
	if _, err := repositories.UpsertROMFile(ctx, big); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(t.TempDir(), "big.zip")
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	err = service.ArchiveDiscs(ctx, "Big", []romdomain.ArchiveDisc{{ROMFile: big, Name: "Big (Disc 1)", Filename: "Big (Disc 1).iso"}}, out)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("RETROXIDE_TEST_ZIP_DIR"); dir != "" {
		if archive, err := os.ReadFile(archivePath); err == nil {
			keepZip(t, "over-4gib", archive)
		}
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entry := reader.File[0]
	if entry.Name != "Big (Disc 1).iso" || entry.UncompressedSize64 != size {
		t.Fatalf("entry %q is %d bytes, want %d", entry.Name, entry.UncompressedSize64, uint64(size))
	}
	content, err := entry.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer content.Close()
	checksum := crc32.NewIEEE()
	if copied, err := io.Copy(checksum, content); err != nil || copied != size {
		t.Fatalf("read back %d bytes, %v", copied, err)
	}
	expected := crc32.NewIEEE()
	if _, err := io.CopyN(expected, zeroReader{}, size); err != nil {
		t.Fatal(err)
	}
	if checksum.Sum32() != expected.Sum32() || entry.CRC32 != expected.Sum32() {
		t.Fatalf("CRC read back %08x, recorded %08x, want %08x", checksum.Sum32(), entry.CRC32, expected.Sum32())
	}
}

func TestCompressingOneDiscLeavesTheOthers(t *testing.T) {
	ctx := context.Background()
	service, repositories, files := compressionFixture(t, discimage.NewCompressor())
	sheet, parts := discFixture(t, files, "cd-cdlz.chd", "Game")
	first, err := service.Assemble(ctx, "Game.cue", sheet, parts, false)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := dataDisc(t, service, files, "Data.cue")
	game := romdomain.ROM{ID: "rom_game", ROMFileID: first.ID, UploaderID: "owner",
		Identity: romdomain.ROMIdentity{Title: "Game", Platform: "psx", IdentifiedBy: "test", Confidence: 100}}
	if err := repositories.SaveROM(ctx, game); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordDisc(ctx, romdomain.ROMDisc{ROMID: game.ID, ROMFileID: second.ID, Number: 2, Filename: "Data.cue"}); err != nil {
		t.Fatal(err)
	}
	woken := 0
	roms := rom.NewService(repositories, files, nil, nil).WithDiscs(service, func() { woken++ })
	owner := identity.Requester{UserID: "owner", Role: identity.RoleUser}

	if _, err := roms.RequestCompression(ctx, owner, game.ID, 1); err != nil {
		t.Fatal(err)
	}
	if compressed, err := service.CompressPending(ctx); err != nil || compressed != 1 {
		t.Fatalf("CompressPending() = %d, %v", compressed, err)
	}
	one, _ := repositories.FindROMFileByID(ctx, first.ID)
	two, _ := repositories.FindROMFileByID(ctx, second.ID)
	if one.Format != romdomain.FormatCHD || two.Format != romdomain.FormatTracks || two.CompressRequested || woken != 1 {
		t.Fatalf("disc 1 is %s, disc 2 is %s (requested %v), woken %d; want chd, tracks", one.Format, two.Format, two.CompressRequested, woken)
	}
	kept, err := repositories.FindROMDiscs(ctx, game.ID)
	if err != nil || len(kept) != 2 || kept[0].ROMFileID != first.ID || kept[1].ROMFileID != second.ID {
		t.Fatalf("discs = %+v, %v; want both on the ROM", kept, err)
	}
	detail, err := roms.Get(ctx, owner, game.ID)
	if err != nil || len(detail.Discs) != 2 || detail.Discs[0].Disc.Format != "chd" || detail.Discs[1].Disc.Format != "tracks" {
		t.Fatalf("detail = %+v, %v", detail.Discs, err)
	}
}
