package uploads

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	romhash "github.com/lcmps/retroxide/infrastructure/hash"
)

type fakeKnownList map[string]biosdomain.Known

func (known fakeKnownList) Lookup(sha1 string, size uint64) (biosdomain.Known, bool) {
	entry, ok := known[strings.ToLower(sha1)]
	return entry, ok && entry.Size == size
}

func (fakeKnownList) Extensions() []string { return []string{"bin", "pce", "rom"} }

// fakeBIOSLookup holds BIOS entries by the stored file they use.
type fakeBIOSLookup map[string]biosdomain.BIOS

func (lookup fakeBIOSLookup) FindBIOSByROMFileID(_ context.Context, romFileID string) (*biosdomain.BIOS, error) {
	entry, ok := lookup[romFileID]
	if !ok {
		return nil, nil
	}
	return &entry, nil
}

func (lookup fakeBIOSLookup) FindBIOS(_ context.Context, id string) (*biosdomain.BIOS, error) {
	for _, entry := range lookup {
		if entry.ID == id {
			return &entry, nil
		}
	}
	return nil, nil
}

const biosContent = "bios bytes"

func biosHash(t *testing.T) romdomain.ROMHash {
	t.Helper()
	hash, err := romhash.New().Hash([]byte(biosContent))
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

type biosFixture struct {
	service *UploadService
	roms    *testROMRepository
	uploads *testUploadRepository
	storage *testStorage
	bioses  fakeBIOSLookup
}

func newBIOSFixture(known fakeKnownList) *biosFixture {
	fixture := &biosFixture{roms: &testROMRepository{}, uploads: &testUploadRepository{}, storage: &testStorage{}, bioses: fakeBIOSLookup{}}
	fixture.service = NewUploadService(fixture.roms, fixture.uploads, fixture.storage, romhash.New(), &testQueue{}, &testAudit{}, UploadPolicy{
		MaxUploadBytes: 1 << 30, AllowedExtensions: []string{"sfc", "bin"},
	}).WithBIOS(known, fixture.bioses)
	return fixture
}

func (fixture *biosFixture) upload(filename string, options BIOSOptions, expectedSize int64) (UploadAcceptedDTO, error) {
	return fixture.service.Create(context.Background(), player, UploadInput{
		UploaderID: player.UserID, Filename: filename, Content: bytes.NewReader([]byte(biosContent)), ExpectedSize: expectedSize, BIOS: options,
	})
}

func TestToggledUploadIsCheckedBeforeStoring(t *testing.T) {
	for _, test := range []struct {
		name     string
		filename string
		options  BIOSOptions
		size     int64
		code     fault.Code
	}{
		{"unknown platform", "scph5501.bin", BIOSOptions{Enabled: true, Platform: "nope"}, 0, fault.CodeValidation},
		{"no platform", "scph5501.bin", BIOSOptions{Enabled: true}, 0, fault.CodeValidation},
		{"type outside the list", "notes.txt", BIOSOptions{Enabled: true, Platform: "ps2"}, 0, fault.CodeUnsupportedUpload},
		{"oversized", "big.bin", BIOSOptions{Enabled: true, Platform: "ps2"}, MaxBIOSUploadBytes + multipartAllowance + 1, fault.CodeUploadTooLarge},
	} {
		fixture := newBIOSFixture(fakeKnownList{})
		if _, err := fixture.upload(test.filename, test.options, test.size); !fault.IsCode(err, test.code) {
			t.Errorf("%s: Create = %v, want %s", test.name, err, test.code)
		}
		if fixture.storage.staged != 0 {
			t.Errorf("%s: %d uploads staged before being refused", test.name, fixture.storage.staged)
		}
	}
}

func TestToggledUploadAcceptsTypesFromTheKnownList(t *testing.T) {
	fixture := newBIOSFixture(fakeKnownList{})
	// .pce is not an allowed game type here, but it is a BIOS type.
	if _, err := fixture.upload("syscard3.pce", BIOSOptions{Enabled: true, Platform: "pcecd"}, 0); err != nil {
		t.Fatal(err)
	}
	saved := fixture.uploads.saved[0]
	if !saved.BIOS || saved.BIOSPlatform == nil || *saved.BIOSPlatform != "pcecd" {
		t.Fatalf("upload = %+v, want marked as a pcecd BIOS", saved)
	}
}

func TestUploadOfStoredGameContentAsBIOSIsAccepted(t *testing.T) {
	fixture := newBIOSFixture(fakeKnownList{})
	fixture.roms.existing = &romdomain.ROMFile{ID: "rf_game", Hash: biosHash(t)}
	if _, err := fixture.upload("scph5501.bin", BIOSOptions{Enabled: true, Platform: "psx"}, 0); err != nil {
		t.Fatalf("toggled upload of content stored as a game = %v", err)
	}
	if fixture.storage.writes != 0 {
		t.Fatal("the content was stored again")
	}
	if len(fixture.uploads.jobs) != 1 || fixture.uploads.jobs[0].ROMFileID != "rf_game" || !fixture.uploads.saved[0].BIOS {
		t.Fatalf("upload not queued against the stored file: %+v %+v", fixture.uploads.saved, fixture.uploads.jobs)
	}
}

func TestRecognizedBIOSUploadedWithoutToggleIsTreatedAsBIOS(t *testing.T) {
	hash := biosHash(t)
	fixture := newBIOSFixture(fakeKnownList{hash.SHA1: {Platform: "psx", Path: "scph5501.bin", SHA1: hash.SHA1, Size: uint64(len(biosContent))}})
	fixture.roms.existing = &romdomain.ROMFile{ID: "rf_game", Hash: hash}
	if _, err := fixture.upload("scph5501.bin", BIOSOptions{}, 0); err != nil {
		t.Fatalf("recognized BIOS stored as a game = %v, want accepted", err)
	}
}

func TestBIOSUploadedTwiceIsAlreadyExists(t *testing.T) {
	hash := biosHash(t)
	known := fakeKnownList{hash.SHA1: {Platform: "psx", Path: "scph5501.bin", SHA1: hash.SHA1, Size: uint64(len(biosContent))}}
	for name, upload := range map[string]struct {
		known   fakeKnownList
		options BIOSOptions
	}{
		"toggled":                {fakeKnownList{}, BIOSOptions{Enabled: true, Platform: "psx"}},
		"recognized, no toggle":  {known, BIOSOptions{}},
		"unknown, as a game too": {fakeKnownList{}, BIOSOptions{}},
	} {
		fixture := newBIOSFixture(upload.known)
		fixture.roms.existing, fixture.roms.unused = &romdomain.ROMFile{ID: "rf_bios", Hash: hash}, true
		fixture.bioses["rf_bios"] = biosdomain.BIOS{ID: "bio_existing", ROMFileID: "rf_bios"}
		_, err := fixture.upload("scph5501.bin", upload.options, 0)
		var appError *fault.Error
		if !fault.IsCode(err, fault.CodeBIOSAlreadyExists) || !asFault(err, &appError) || appError.ExistingBIOSID == nil || *appError.ExistingBIOSID != "bio_existing" {
			t.Errorf("%s: Create = %v, want bios_already_exists for bio_existing", name, err)
		}
	}
}

func TestCheckSessionDuplicateBIOSRules(t *testing.T) {
	ctx := context.Background()
	hash := biosHash(t)
	for _, test := range []struct {
		name     string
		toggled  bool
		biosFile bool
		want     fault.Code
	}{
		{"toggled, already a BIOS", true, true, fault.CodeBIOSAlreadyExists},
		{"not toggled, already a BIOS", false, true, fault.CodeBIOSAlreadyExists},
		// Content stored only as a game does not stop it becoming a BIOS.
		{"toggled, stored as a game", true, false, ""},
		{"not toggled, stored as a game", false, false, fault.CodeROMAlreadyExists},
	} {
		fixture := newSessionFixture(&testStorage{})
		bioses := fakeBIOSLookup{}
		fixture.service.WithBIOS(fakeKnownList{}, bioses)
		fixture.roms.existing = &romdomain.ROMFile{ID: "rf_stored", Hash: hash}
		if test.biosFile {
			fixture.roms.unused = true
			bioses["rf_stored"] = biosdomain.BIOS{ID: "bio_stored", ROMFileID: "rf_stored"}
		}
		input := CreateSessionInput{Filename: "game.sfc", Size: int64(len(biosContent))}
		if test.toggled {
			input = CreateSessionInput{Filename: "scph5501.bin", Size: int64(len(biosContent)), BIOS: true, Platform: "psx"}
		}
		session, err := fixture.service.CreateSession(ctx, player, input)
		if err != nil {
			t.Fatal(err)
		}
		err = fixture.service.CheckSessionDuplicate(ctx, player, session.SessionID, hash.SHA256)
		if (test.want == "" && err != nil) || (test.want != "" && !fault.IsCode(err, test.want)) {
			t.Errorf("%s: check = %v, want %q", test.name, err, test.want)
		}
	}
}

func TestSessionKeepsBIOSOptions(t *testing.T) {
	ctx := context.Background()
	fixture := newSessionFixture(&testStorage{})
	fixture.service.WithBIOS(fakeKnownList{}, fakeBIOSLookup{})
	if _, err := fixture.service.CreateSession(ctx, player, CreateSessionInput{Filename: "notes.txt", Size: 4, BIOS: true, Platform: "ps2"}); !fault.IsCode(err, fault.CodeUnsupportedUpload) {
		t.Fatalf("BIOS session for a .txt = %v, want refused", err)
	}
	if _, err := fixture.service.CreateSession(ctx, player, CreateSessionInput{Filename: "a.bin", Size: 4, BIOS: true, Platform: "ps2", Part: true}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("BIOS session for a disc part = %v, want refused", err)
	}
	session, err := fixture.service.CreateSession(ctx, player, CreateSessionInput{Filename: "ps2.bin", Size: int64(len(biosContent)), BIOS: true, Platform: "ps2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 0, strings.NewReader(biosContent)); err != nil {
		t.Fatal(err)
	}
	saved := fixture.uploads.saved[len(fixture.uploads.saved)-1]
	if !saved.BIOS || saved.BIOSPlatform == nil || *saved.BIOSPlatform != "ps2" {
		t.Fatalf("completed upload = %+v, want a ps2 BIOS", saved)
	}
}

func asFault(err error, target **fault.Error) bool {
	appError, ok := err.(*fault.Error)
	*target = appError
	return ok
}
