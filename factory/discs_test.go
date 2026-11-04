package factory

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/discs"
	"github.com/lcmps/retroxide/application/fault"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"github.com/lcmps/retroxide/infrastructure/chd"
	"github.com/lcmps/retroxide/infrastructure/discimage"
	romhash "github.com/lcmps/retroxide/infrastructure/hash"
	"github.com/lcmps/retroxide/infrastructure/persistence"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

const chdFixtures = "../infrastructure/chd/testdata"

// discTracks rebuilds the fixture disc's track files from its CHD, as the files a person
// would upload.
func discTracks(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(chdFixtures, "chd", "cd-cdlz.chd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := chd.Open(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	disc, err := reader.Disc()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, track := range disc.Tracks {
		var data bytes.Buffer
		if _, err := reader.ExtractTrack(track, &data); err != nil {
			t.Fatal(err)
		}
		files[disc.TrackFileName("Game", track)] = data.Bytes()
	}
	return files, disc.Sheet("Game")
}

func zipHashes(t *testing.T, archive []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]string{}
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(entry)
		sum := sha1.Sum(data)
		hashes[file.Name] = hex.EncodeToString(sum[:])
	}
	return hashes
}

// TestDiscsAcrossForms follows a disc through upload as CUE/BIN, duplicate detection against
// the same disc as a CHD, download as tracks, and (with chdman) compression to CHD.
func TestDiscsAcrossForms(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := persistence.OpenDatabase(ctx, "sqlite", filepath.Join(directory, "db.sqlite3"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repositories := persistence.NewRepositories(database)
	files := storage.New(filepath.Join(directory, "storage"))
	var compressor discs.Compressor
	if path := os.Getenv("RETROXIDE_TEST_CHDMAN"); path != "" {
		chdman, ok := discimage.FindChdman(path)
		if !ok {
			t.Fatalf("chdman not usable at %s", path)
		}
		compressor = chdman
	}
	service := discs.NewService(repositories, files, romhash.New(), discimage.NewImages(romhash.New()), compressor)

	tracks, sheet := discTracks(t)
	var parts []discs.Part
	for name, data := range tracks {
		hash, _ := romhash.New().Hash(data)
		key, err := files.Write(ctx, hash.SHA256, data)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, discs.Part{Name: name, StoragePath: key, Size: int64(len(data)), Hash: hash})
	}
	romFile, err := service.Assemble(ctx, "Game.cue", sheet, parts, compressor != nil)
	if err != nil {
		t.Fatal(err)
	}
	if romFile.Format != romdomain.FormatTracks || romFile.DiscIdentity == nil {
		t.Fatalf("assembled file = %+v", romFile)
	}
	stored, err := repositories.FindDiscTracks(ctx, romFile.ID)
	if err != nil || len(stored) != 4 || stored[1].Mode != "AUDIO" || !stored[1].PregapStored {
		t.Fatalf("stored tracks = %+v, %v", stored, err)
	}

	// The same disc as a CHD is the same disc.
	content, _ := os.ReadFile(filepath.Join(chdFixtures, "chd", "cd-cdzl.chd"))
	chdHash, _ := romhash.New().Hash(content)
	chdKey, err := files.Write(ctx, chdHash.SHA256, content)
	if err != nil {
		t.Fatal(err)
	}
	chdFile, err := repositories.UpsertROMFile(ctx, romdomain.ROMFile{ID: "rf_" + chdHash.SHA256, Hash: chdHash, Size: uint64(len(content)), StoragePath: chdKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Index(ctx, chdFile, "Game"); !fault.IsCode(err, fault.CodeROMAlreadyExists) {
		t.Fatalf("indexing the same disc as a CHD = %v, want rom already exists", err)
	}
	// Uploading the CUE/BIN set again is refused too.
	if _, err := service.Assemble(ctx, "Game.cue", sheet, parts, false); !fault.IsCode(err, fault.CodeROMAlreadyExists) {
		t.Fatalf("assembling the disc again = %v, want rom already exists", err)
	}

	want := map[string]string{}
	for name, data := range tracks {
		sum := sha1.Sum(data)
		want[name] = hex.EncodeToString(sum[:])
	}
	checkArchive := func(label string) {
		t.Helper()
		current, err := repositories.FindROMFileByID(ctx, romFile.ID)
		if err != nil {
			t.Fatal(err)
		}
		var archive bytes.Buffer
		if err := service.Archive(ctx, *current, "Game", &archive); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		hashes := zipHashes(t, archive.Bytes())
		for name, sum := range want {
			if hashes[name] != sum {
				t.Errorf("%s: %s = %s, want %s", label, name, hashes[name], sum)
			}
		}
		if _, ok := hashes["Game.cue"]; !ok {
			t.Errorf("%s: archive lacks the sheet: %v", label, hashes)
		}
	}
	checkArchive("tracks")

	if compressor == nil {
		t.Log("RETROXIDE_TEST_CHDMAN not set; skipping compression")
		return
	}
	compressed, err := service.CompressPending(ctx)
	if err != nil || compressed != 1 {
		t.Fatalf("CompressPending() = %d, %v", compressed, err)
	}
	after, _ := repositories.FindROMFileByID(ctx, romFile.ID)
	if after.Format != romdomain.FormatCHD || after.StoragePath == romFile.StoragePath || *after.DiscIdentity != *romFile.DiscIdentity {
		t.Fatalf("compressed file = %+v", after)
	}
	head, err := files.Read(ctx, after.StoragePath)
	if err != nil || !strings.HasPrefix(string(head), "MComprHD") {
		t.Fatalf("stored content is not a CHD: %v", err)
	}
	if left, _ := repositories.FindDiscTracks(ctx, romFile.ID); left[0].StoragePath != nil {
		t.Fatal("tracks still point at their separate files")
	}
	checkArchive("chd")
}

func TestSingleFileDiscsAreSplitIntoTracks(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := persistence.OpenDatabase(ctx, "sqlite", filepath.Join(directory, "db.sqlite3"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repositories := persistence.NewRepositories(database)
	files := storage.New(filepath.Join(directory, "storage"))
	service := discs.NewService(repositories, files, romhash.New(), discimage.NewImages(romhash.New()), nil)

	tracks, _ := discTracks(t)
	// One file holding all four tracks, with the pregaps where their INDEX 00 says.
	var single bytes.Buffer
	var offsets [4]int
	for number := 1; number <= 4; number++ {
		offsets[number-1] = single.Len() / 2352
		single.Write(tracks[fmt.Sprintf("Game (Track %d).bin", number)])
	}
	sheet := fmt.Sprintf(`FILE "Game.bin" BINARY
  TRACK 01 MODE1/2352
    INDEX 01 %s
  TRACK 02 AUDIO
    INDEX 00 %s
    INDEX 01 %s
  TRACK 03 MODE2/2352
    INDEX 01 %s
  TRACK 04 AUDIO
    INDEX 00 %s
    INDEX 01 %s
`, msfOf(offsets[0]), msfOf(offsets[1]), msfOf(offsets[1]+30), msfOf(offsets[2]), msfOf(offsets[3]), msfOf(offsets[3]+30))
	hash, _ := romhash.New().Hash(single.Bytes())
	key, err := files.Write(ctx, hash.SHA256, single.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	romFile, err := service.Assemble(ctx, "Game.cue", sheet, []discs.Part{{Name: "Game.bin", StoragePath: key, Size: int64(single.Len()), Hash: hash}}, false)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repositories.FindDiscTracks(ctx, romFile.ID)
	if err != nil || len(stored) != 4 {
		t.Fatalf("tracks = %+v, %v", stored, err)
	}
	for _, track := range stored {
		name := fmt.Sprintf("Game (Track %d).bin", track.Number)
		sum := sha1.Sum(tracks[name])
		if track.Hash.SHA1 != hex.EncodeToString(sum[:]) || track.Filename != "Game.bin" {
			t.Errorf("track %d = %s %s, want %s split exactly, from Game.bin", track.Number, track.Filename, track.Hash.SHA1, name)
		}
	}
	// Downloading gives back exactly what was uploaded: the sheet and the single file.
	var archive bytes.Buffer
	if err := service.Archive(ctx, romFile, "Game", &archive); err != nil {
		t.Fatal(err)
	}
	reader, _ := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if len(reader.File) != 2 {
		t.Fatalf("archive holds %d files, want the sheet and Game.bin", len(reader.File))
	}
	entry, _ := reader.Open("Game.cue")
	written, _ := io.ReadAll(entry)
	if string(written) != sheet {
		t.Errorf("sheet:\n%s\nwant the uploaded one:\n%s", written, sheet)
	}
	entry, _ = reader.Open("Game.bin")
	joined, _ := io.ReadAll(entry)
	if !bytes.Equal(joined, single.Bytes()) {
		t.Error("Game.bin is not the uploaded file")
	}
}

func msfOf(frames int) string {
	return fmt.Sprintf("%02d:%02d:%02d", frames/4500, frames/75%60, frames%75)
}

func TestFilesStoredBeforeDiscsAreInspected(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := persistence.OpenDatabase(ctx, "sqlite", filepath.Join(directory, "db.sqlite3"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repositories := persistence.NewRepositories(database)
	files := storage.New(filepath.Join(directory, "storage"))
	service := discs.NewService(repositories, files, romhash.New(), discimage.NewImages(romhash.New()), nil)

	store := func(content []byte) romdomain.ROMFile {
		hash, _ := romhash.New().Hash(content)
		key, err := files.Write(ctx, hash.SHA256, content)
		if err != nil {
			t.Fatal(err)
		}
		file, err := repositories.UpsertROMFile(ctx, romdomain.ROMFile{ID: "rf_" + hash.SHA256, Hash: hash, Size: uint64(len(content)), StoragePath: key, Format: romdomain.FormatPending})
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	chdContent, _ := os.ReadFile(filepath.Join(chdFixtures, "chd", "cd-cdzs.chd"))
	disc := store(chdContent)
	cartridge := store([]byte("a cartridge ROM"))
	if indexed, err := service.IndexPending(ctx); err != nil || indexed != 2 {
		t.Fatalf("IndexPending() = %d, %v", indexed, err)
	}
	if found, _ := repositories.FindROMFileByID(ctx, disc.ID); found.Format != romdomain.FormatCHD || found.DiscIdentity == nil {
		t.Fatalf("stored CHD = %+v, want indexed", found)
	}
	if tracks, _ := repositories.FindDiscTracks(ctx, disc.ID); len(tracks) != 4 {
		t.Fatalf("stored CHD has %d tracks, want 4", len(tracks))
	}
	if found, _ := repositories.FindROMFileByID(ctx, cartridge.ID); found.Format != romdomain.FormatFile {
		t.Fatalf("cartridge = %+v, want a plain file", found)
	}
	if indexed, _ := service.IndexPending(ctx); indexed != 0 {
		t.Fatal("files were inspected twice")
	}
}

func TestSingleTrackDataDiscsDownloadAsISO(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := persistence.OpenDatabase(ctx, "sqlite", filepath.Join(directory, "db.sqlite3"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repositories := persistence.NewRepositories(database)
	files := storage.New(filepath.Join(directory, "storage"))
	service := discs.NewService(repositories, files, romhash.New(), discimage.NewImages(romhash.New()), nil)

	tracks, _ := discTracks(t)
	data := tracks["Game (Track 1).bin"] // the fixture's MODE1/2352 track
	hash, _ := romhash.New().Hash(data)
	key, err := files.Write(ctx, hash.SHA256, data)
	if err != nil {
		t.Fatal(err)
	}
	romFile, err := service.Assemble(ctx, "Data.cue", "FILE \"Data.bin\" BINARY\n  TRACK 01 MODE1/2352\n    INDEX 01 00:00:00\n",
		[]discs.Part{{Name: "Data.bin", StoragePath: key, Size: int64(len(data)), Hash: hash}}, false)
	if err != nil {
		t.Fatal(err)
	}
	var iso bytes.Buffer
	if err := service.ISO(ctx, romFile, &iso); err != nil {
		t.Fatal(err)
	}
	if iso.Len() != len(data)/2352*2048 {
		t.Fatalf("ISO is %d bytes, want %d", iso.Len(), len(data)/2352*2048)
	}
	for sector := 0; sector < len(data)/2352; sector++ {
		if !bytes.Equal(iso.Bytes()[sector*2048:(sector+1)*2048], data[sector*2352+16:sector*2352+2064]) {
			t.Fatalf("sector %d's user data differs", sector)
		}
	}

	// A disc with audio tracks would lose them: no ISO.
	all, sheet := discTracks(t)
	var parts []discs.Part
	for name, content := range all {
		hash, _ := romhash.New().Hash(content)
		key, _ := files.Write(ctx, hash.SHA256, content)
		parts = append(parts, discs.Part{Name: name, StoragePath: key, Size: int64(len(content)), Hash: hash})
	}
	mixed, err := service.Assemble(ctx, "Game.cue", sheet, parts, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ISO(ctx, mixed, io.Discard); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("ISO of a mixed-mode disc = %v, want refused", err)
	}
}
