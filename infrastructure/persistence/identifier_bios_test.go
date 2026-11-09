package persistence

import (
	"context"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/uploads"
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	"github.com/lcmps/retroxide/domain/identity"
	"github.com/lcmps/retroxide/infrastructure/hash"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

type testKnownList map[string]biosdomain.Known

func (known testKnownList) Lookup(sha1 string, size uint64) (biosdomain.Known, bool) {
	entry, ok := known[strings.ToLower(sha1)]
	return entry, ok && entry.Size == size
}

func (testKnownList) Extensions() []string { return []string{"bin"} }

type biosIdentification struct {
	repositories *Repositories
	identifier   *Identifier
	uploader     *uploads.UploadService
	ctx          context.Context
}

// newBIOSIdentification knows one PS2 BIOS: the file with knownContent.
func newBIOSIdentification(t *testing.T, knownContent string) *biosIdentification {
	t.Helper()
	repositories, ctx := openTestRepositories(t)
	digest, err := hash.New().Hash([]byte(knownContent))
	if err != nil {
		t.Fatal(err)
	}
	region, version := "Europe", "1.60"
	known := testKnownList{digest.SHA1: {
		Platform: "ps2", Path: "pcsx2/bios/ps2-0160e-20010704.bin", Size: uint64(len(knownContent)), SHA1: digest.SHA1,
		Labels: biosdomain.Labels{Region: &region, Version: &version},
	}}
	uploader := uploads.NewUploadService(repositories, repositories, storage.New(t.TempDir()), hash.New(), nil, nil,
		uploads.UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"bin"}}).WithBIOS(known, repositories)
	return &biosIdentification{repositories: repositories, identifier: NewIdentifier(repositories.DB).WithBIOS(known), uploader: uploader, ctx: ctx}
}

// upload sends content and returns its upload and stored file IDs.
func (fixture *biosIdentification) upload(t *testing.T, filename, content string, options uploads.BIOSOptions) (string, string) {
	t.Helper()
	accepted, err := fixture.uploader.Create(fixture.ctx, identity.Requester{UserID: "owner", Role: identity.RoleUser},
		uploads.UploadInput{Filename: filename, Content: strings.NewReader(content), BIOS: options})
	if err != nil {
		t.Fatal(err)
	}
	var job JobQueueModel
	if err := fixture.repositories.DB.Where("upload_id = ?", accepted.UploadID).Take(&job).Error; err != nil {
		t.Fatal(err)
	}
	return accepted.UploadID, job.ROMFileID
}

func (fixture *biosIdentification) onlyBIOS(t *testing.T, uploadID string) biosdomain.BIOS {
	t.Helper()
	assertDeleteCount(t, fixture.repositories, &ROMModel{}, 0)
	upload, err := fixture.repositories.FindUpload(fixture.ctx, uploadID)
	if err != nil || upload == nil || upload.Status != "completed" || upload.ROMID != nil || upload.BIOSID == nil {
		t.Fatalf("upload = %+v, %v; want completed as a BIOS", upload, err)
	}
	entry, err := fixture.repositories.FindBIOS(fixture.ctx, *upload.BIOSID)
	if err != nil || entry == nil {
		t.Fatalf("BIOS %s = %+v, %v", *upload.BIOSID, entry, err)
	}
	return *entry
}

func TestIdentifyRecognizedBIOS(t *testing.T) {
	fixture := newBIOSIdentification(t, "known ps2 bios")
	uploadID, fileID := fixture.upload(t, "my-ps2.bin", "known ps2 bios", uploads.BIOSOptions{})
	if err := fixture.identifier.Identify(fixture.ctx, uploadID, fileID); err != nil {
		t.Fatal(err)
	}
	entry := fixture.onlyBIOS(t, uploadID)
	if entry.Platform != "ps2" || !entry.Recognized || entry.ExpectedPath == nil || *entry.ExpectedPath != "pcsx2/bios/ps2-0160e-20010704.bin" ||
		entry.Labels.Version == nil || *entry.Labels.Version != "1.60" || entry.Filename != "my-ps2.bin" || entry.UploaderID != "owner" {
		t.Fatalf("recognized BIOS = %+v", entry)
	}
	// Identifying the same upload again changes nothing.
	if err := fixture.identifier.Identify(fixture.ctx, uploadID, fileID); err != nil {
		t.Fatal(err)
	}
	assertDeleteCount(t, fixture.repositories, &BIOSModel{}, 1)
}

func TestIdentifyToggledUnknownBIOS(t *testing.T) {
	fixture := newBIOSIdentification(t, "known ps2 bios")
	uploadID, fileID := fixture.upload(t, "dump.bin", "some other dump", uploads.BIOSOptions{Enabled: true, Platform: "psx"})
	if err := fixture.identifier.Identify(fixture.ctx, uploadID, fileID); err != nil {
		t.Fatal(err)
	}
	entry := fixture.onlyBIOS(t, uploadID)
	if entry.Platform != "psx" || entry.Recognized || entry.ExpectedPath != nil {
		t.Fatalf("unrecognized BIOS = %+v", entry)
	}
}

func TestIdentifyToggledButRecognizedUsesListPlatform(t *testing.T) {
	fixture := newBIOSIdentification(t, "known ps2 bios")
	uploadID, fileID := fixture.upload(t, "wrong.bin", "known ps2 bios", uploads.BIOSOptions{Enabled: true, Platform: "psx"})
	if err := fixture.identifier.Identify(fixture.ctx, uploadID, fileID); err != nil {
		t.Fatal(err)
	}
	if entry := fixture.onlyBIOS(t, uploadID); entry.Platform != "ps2" || !entry.Recognized {
		t.Fatalf("BIOS = %+v, want the list's ps2", entry)
	}
}

func TestIdentifierSkipsDiscIndexingForBIOS(t *testing.T) {
	fixture := newBIOSIdentification(t, "known ps2 bios")
	// Indexing fails every file it sees, so identifying passes only if it never indexes.
	fixture.identifier.WithDiscs(failingDiscIndexer{})
	uploadID, fileID := fixture.upload(t, "track.bin", "a toggled bin", uploads.BIOSOptions{Enabled: true, Platform: "ps2"})
	if err := fixture.identifier.Identify(fixture.ctx, uploadID, fileID); err != nil {
		t.Fatalf("Identify = %v; a BIOS must not be indexed as a disc", err)
	}
	fixture.onlyBIOS(t, uploadID)
}

func TestCompleteWithFilenameMakesABIOS(t *testing.T) {
	fixture := newBIOSIdentification(t, "known ps2 bios")
	uploadID, fileID := fixture.upload(t, "dump.bin", "fallback dump", uploads.BIOSOptions{Enabled: true, Platform: "ps2"})
	if err := fixture.identifier.CompleteWithFilename(fixture.ctx, uploadID, fileID); err != nil {
		t.Fatal(err)
	}
	fixture.onlyBIOS(t, uploadID)
}
