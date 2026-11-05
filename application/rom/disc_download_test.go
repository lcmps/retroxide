package rom

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

var discOwner = identity.Requester{UserID: "owner", Role: identity.RoleUser}

type auditedDownload struct{ romID, romFileID string }

type recordingAudit struct {
	romTestAudit
	served []auditedDownload
}

func (audit *recordingAudit) DownloadServed(_ context.Context, romID, romFileID, _ string, _ uint64) {
	audit.served = append(audit.served, auditedDownload{romID, romFileID})
}

func discFile(id string, format romdomain.FileFormat, sheet bool) romdomain.ROMFile {
	file := romdomain.ROMFile{ID: id, StoragePath: "/stored/" + id, Format: format, Size: uint64(len(id)),
		Hash: romdomain.ROMHash{SHA256: "sha256-" + id, SHA1: "sha1-" + id, MD5: "md5-" + id, CRC32: "crc-" + id}}
	if sheet {
		path := "/stored/" + id + ".cue"
		file.SheetPath = &path
	}
	return file
}

// titleService is a PSX title of two discs: disc 1 uploaded as "A (Disc 1).cue" and kept as
// tracks, disc 2 uploaded as "A (Disc 2).chd".
func titleService(t *testing.T) (*Service, *romTestRepository, *fakeDiscs, *recordingAudit, *int) {
	t.Helper()
	repository := &romTestRepository{
		roms: map[string]romdomain.ROM{"ff7": {ID: "ff7", ROMFileID: "d1", UploaderID: "owner",
			Identity: romdomain.ROMIdentity{Title: "Final Fantasy VII", Platform: "psx"}}},
		romFiles: map[string]romdomain.ROMFile{
			"d1": discFile("d1", romdomain.FormatTracks, true),
			"d2": discFile("d2", romdomain.FormatCHD, false),
		},
		discs: map[string][]romdomain.ROMDisc{"ff7": {
			{ROMID: "ff7", ROMFileID: "d1", Number: 1, Filename: "A (Disc 1).cue"},
			{ROMID: "ff7", ROMFileID: "d2", Number: 2, Filename: "A (Disc 2).chd"},
		}},
		discTracks: map[string][]romdomain.DiscTrack{
			"d1": {{Number: 1, Mode: "MODE2/2352", Size: 100, Hash: romdomain.ROMHash{SHA1: "t1"}}, {Number: 2, Mode: "AUDIO", Size: 50}},
			"d2": {{Number: 1, Mode: "MODE2/2352", Size: 200, Hash: romdomain.ROMHash{SHA1: "t2"}}},
		},
	}
	storage := &romTestStorage{files: map[string][]byte{"/stored/d1": []byte("one"), "/stored/d2": []byte("two")}}
	discs := &fakeDiscs{canCompress: true, cannot: map[string]bool{"d2": true}}
	audit := &recordingAudit{}
	woken := new(int)
	service := NewService(repository, storage, audit, nil).WithDiscs(discs, func() { *woken++ })
	return service, repository, discs, audit, woken
}

func formatList(options []DownloadOptionDTO) string {
	var names []string
	for _, option := range options {
		names = append(names, string(option.Format)+option.Extension)
	}
	return strings.Join(names, " ")
}

func TestSingleDiscDetailsAreUnchanged(t *testing.T) {
	service, repository, _, _, _ := titleService(t)
	repository.discs = nil
	repository.uploadedFilenames = map[string]string{"ff7": "A.cue"}
	detail, err := service.Get(context.Background(), discOwner, "ff7")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Discs) != 1 {
		t.Fatalf("discs = %+v, want one", detail.Discs)
	}
	entry := detail.Discs[0]
	if detail.Hash.SHA1 != "sha1-d1" || entry.Hash != detail.Hash || entry.Number != 1 || entry.ROMFileID != "d1" || entry.Filename != "A.cue" || entry.Size != 2 {
		t.Fatalf("detail hash %+v, entry %+v", detail.Hash, entry)
	}
	if detail.Disc == nil || len(detail.Disc.Tracks) != 2 || len(entry.Disc.Tracks) != 2 || detail.Disc.Tracks[0].SHA1 != "t1" {
		t.Fatalf("disc = %+v, entry disc = %+v", detail.Disc, entry.Disc)
	}
	if got := formatList(detail.Downloads); got != "tracks.zip" || formatList(entry.Downloads) != got {
		t.Fatalf("downloads = %q, entry %q; want the tracks zip alone", got, formatList(entry.Downloads))
	}
}

func TestMultiDiscDetails(t *testing.T) {
	service, repository, discs, _, _ := titleService(t)
	file := repository.romFiles["d2"]
	file.CompressRequested = true
	repository.romFiles["d2"] = file
	discs.percent, discs.running = 42, true

	detail, err := service.Get(context.Background(), discOwner, "ff7")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Discs) != 2 || detail.Discs[0].Number != 1 || detail.Discs[1].Number != 2 {
		t.Fatalf("discs = %+v", detail.Discs)
	}
	first, second := detail.Discs[0], detail.Discs[1]
	if first.Hash.SHA1 != "sha1-d1" || second.Hash.SHA1 != "sha1-d2" || second.ROMFileID != "d2" {
		t.Fatalf("hashes = %+v, %+v", first.Hash, second.Hash)
	}
	if detail.Hash != first.Hash || detail.Disc == nil || detail.Disc.Tracks[0].SHA1 != "t1" {
		t.Fatalf("top level hash %+v and disc %+v are not the primary's", detail.Hash, detail.Disc)
	}
	if first.Disc.Format != "tracks" || second.Disc.Format != "chd" || second.Disc.Tracks[0].SHA1 != "t2" {
		t.Fatalf("disc formats = %+v, %+v", first.Disc, second.Disc)
	}
	if !first.Disc.CanCompress || second.Disc.CanCompress {
		t.Errorf("can compress = %v, %v; want true, false", first.Disc.CanCompress, second.Disc.CanCompress)
	}
	if first.Disc.CompressionRequested || !second.Disc.CompressionRequested || first.Disc.CompressionPercent != nil ||
		second.Disc.CompressionPercent == nil || *second.Disc.CompressionPercent != 42 {
		t.Errorf("compression state = %+v, %+v", first.Disc, second.Disc)
	}
	if len(detail.Downloads) != 1 || detail.Downloads[0] != (DownloadOptionDTO{Format: DownloadAll, Extension: ".zip"}) {
		t.Fatalf("downloads = %+v, want only the zip of all discs", detail.Downloads)
	}
	if formatList(first.Downloads) != "tracks.zip" || formatList(second.Downloads) != "chd.chd tracks.zip" {
		t.Errorf("per disc downloads = %q, %q", formatList(first.Downloads), formatList(second.Downloads))
	}
}

func TestEachDiscUsesItsOwnFilename(t *testing.T) {
	service, _, _, _, _ := titleService(t)
	detail, err := service.Get(context.Background(), discOwner, "ff7")
	if err != nil {
		t.Fatal(err)
	}
	if got := detail.Discs[1].Downloads[0]; got.Format != DownloadCHD {
		t.Fatalf("disc 2 offers %+v first, want chd", got)
	}
	if got := detail.Discs[0].Downloads[0]; got.Format != DownloadTracks {
		t.Fatalf("disc 1 offers %+v first, want tracks", got)
	}
	if detail.Discs[0].Filename != "A (Disc 1).cue" || detail.Discs[1].Filename != "A (Disc 2).chd" {
		t.Fatalf("filenames = %q, %q", detail.Discs[0].Filename, detail.Discs[1].Filename)
	}
}

func TestDownloadNames(t *testing.T) {
	rom := romdomain.ROM{ID: "ff7", Identity: romdomain.ROMIdentity{Title: "Final Fantasy VII", Platform: "psx"}}
	tests := []struct {
		name   string
		number int
		count  int
		want   string
	}{
		{"one disc numbered 1", 1, 1, "Final Fantasy VII.chd"},
		{"disc 2 of a title", 2, 2, "Final Fantasy VII (Disc 2).chd"},
		{"disc 1 of a title", 1, 2, "Final Fantasy VII (Disc 1).chd"},
		{"a lone disc numbered 2", 2, 1, "Final Fantasy VII (Disc 2).chd"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			disc := romdomain.ROMDisc{Number: test.number, Filename: "ff7 disc.CHD"}
			if got := discDownloadName(rom, disc, test.count); got != test.want {
				t.Fatalf("discDownloadName() = %q, want %q", got, test.want)
			}
		})
	}

	t.Run("all", func(t *testing.T) {
		service, _, _, _, _ := titleService(t)
		got, err := service.DownloadFilename(context.Background(), discOwner, "ff7", DownloadAll, 0)
		if err != nil || got != "Final Fantasy VII.zip" {
			t.Fatalf("DownloadFilename(all) = %q, %v", got, err)
		}
		if got, err := service.DownloadFilename(context.Background(), discOwner, "ff7", "", 2); err != nil || got != "Final Fantasy VII (Disc 2).chd" {
			t.Fatalf("DownloadFilename(disc 2) = %q, %v", got, err)
		}
	})
}

func TestMultiDiscDefaultDownloadIsEveryDisc(t *testing.T) {
	service, _, _, audit, _ := titleService(t)
	output, err := service.DownloadAs(context.Background(), discOwner, "ff7", "", 0)
	if err != nil || output.Archive == nil || output.Filename != "Final Fantasy VII.zip" || output.MIMEType != "application/zip" {
		t.Fatalf("DownloadAs() = %+v, %v", output, err)
	}
	var body bytes.Buffer
	if err := output.Archive(&body); err != nil {
		t.Fatal(err)
	}
	if got, want := body.String(), "Final Fantasy VII: Final Fantasy VII (Disc 1)/, Final Fantasy VII (Disc 2).chd"; got != want {
		t.Fatalf("archived %q, want %q", got, want)
	}
	output.Served(context.Background(), 10)
	if len(audit.served) != 1 || audit.served[0] != (auditedDownload{"ff7", "d1"}) {
		t.Fatalf("audit = %+v", audit.served)
	}
	if viaDefault, err := service.Download(context.Background(), discOwner, "ff7"); err != nil || viaDefault.Filename != output.Filename {
		t.Fatalf("Download() = %+v, %v", viaDefault, err)
	}
}

func TestMultiDiscFormatNeedsADisc(t *testing.T) {
	service, repository, _, _, _ := titleService(t)
	if _, err := service.DownloadAs(context.Background(), discOwner, "ff7", DownloadCHD, 0); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("chd without a disc = %v, want a validation error", err)
	}
	if _, err := service.DownloadAs(context.Background(), discOwner, "ff7", "", 3); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("unknown disc = %v, want not found", err)
	}
	if _, err := service.DownloadFilename(context.Background(), discOwner, "ff7", "", 3); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("unknown disc name = %v, want not found", err)
	}
	if _, err := service.DownloadAs(context.Background(), discOwner, "ff7", DownloadAll, 2); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("all of one disc = %v, want a validation error", err)
	}

	repository.discs = map[string][]romdomain.ROMDisc{"ff7": repository.discs["ff7"][:1]}
	if _, err := service.DownloadAs(context.Background(), discOwner, "ff7", DownloadAll, 0); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("all on a single disc = %v, want a validation error", err)
	}
	if _, err := service.DownloadFilename(context.Background(), discOwner, "ff7", DownloadAll, 0); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("all on a single disc name = %v, want a validation error", err)
	}
}

func TestChoosingADiscDownloadsOnlyThatDisc(t *testing.T) {
	service, _, _, audit, _ := titleService(t)
	for _, format := range []DownloadFormat{"", DownloadCHD} {
		output, err := service.DownloadAs(context.Background(), discOwner, "ff7", format, 2)
		if err != nil || output.Content.ReadSeekCloser == nil || output.Filename != "Final Fantasy VII (Disc 2).chd" || output.SHA256 != "sha256-d2" {
			t.Fatalf("DownloadAs(%q, disc 2) = %+v, %v", format, output, err)
		}
		content, _ := io.ReadAll(output.Content)
		output.Content.Close()
		if string(content) != "two" {
			t.Fatalf("content = %q, want disc 2's file", content)
		}
		output.Served(context.Background(), 3)
	}
	if len(audit.served) != 2 || audit.served[0] != (auditedDownload{"ff7", "d2"}) || audit.served[1] != audit.served[0] {
		t.Fatalf("audit = %+v, want disc 2's file twice", audit.served)
	}
	output, err := service.DownloadAs(context.Background(), discOwner, "ff7", "", 1)
	if err != nil || output.Archive == nil || output.Filename != "Final Fantasy VII (Disc 1).zip" {
		t.Fatalf("disc 1 = %+v, %v", output, err)
	}
}

func TestCompressingATitle(t *testing.T) {
	service, repository, discs, _, woken := titleService(t)
	// Only disc 1 can compress; disc 2 is skipped.
	detail, err := service.RequestCompression(context.Background(), discOwner, "ff7", 0)
	if err != nil || strings.Join(discs.requested, ",") != "d1" || *woken != 1 {
		t.Fatalf("RequestCompression(0) = %v; requested %v, woken %d", err, discs.requested, *woken)
	}
	if len(detail.Discs) != 2 {
		t.Fatalf("detail discs = %+v", detail.Discs)
	}

	// Both can: every disc is requested, with one wake.
	discs.requested, *woken = nil, 0
	discs.cannot = nil
	repository.romFiles["d2"] = discFile("d2", romdomain.FormatTracks, true)
	if _, err := service.RequestCompression(context.Background(), discOwner, "ff7", 0); err != nil || strings.Join(discs.requested, ",") != "d1,d2" || *woken != 1 {
		t.Fatalf("RequestCompression(0) = %v; requested %v, woken %d", err, discs.requested, *woken)
	}

	// A chosen disc is the only one.
	discs.requested, *woken = nil, 0
	if _, err := service.RequestCompression(context.Background(), discOwner, "ff7", 2); err != nil || strings.Join(discs.requested, ",") != "d2" || *woken != 1 {
		t.Fatalf("RequestCompression(2) = %v; requested %v, woken %d", err, discs.requested, *woken)
	}
	if _, err := service.RequestCompression(context.Background(), discOwner, "ff7", 9); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("unknown disc = %v, want not found", err)
	}

	// None can: a validation error, and nothing is woken.
	discs.requested, *woken = nil, 0
	discs.canCompress = false
	if _, err := service.RequestCompression(context.Background(), discOwner, "ff7", 0); !fault.IsCode(err, fault.CodeValidation) || len(discs.requested) != 0 || *woken != 0 {
		t.Fatalf("none can compress = %v; requested %v, woken %d", err, discs.requested, *woken)
	}
}
