package uploads

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
	romhash "github.com/lcmps/retroxide/infrastructure/hash"
)

type memorySessions struct {
	sessions map[string]uploadsdomain.Session
}

func (repository *memorySessions) SaveUploadSession(_ context.Context, session uploadsdomain.Session) error {
	repository.sessions[session.ID] = session
	return nil
}

func (repository *memorySessions) FindUploadSession(_ context.Context, sessionID string) (*uploadsdomain.Session, error) {
	session, ok := repository.sessions[sessionID]
	if !ok {
		return nil, nil
	}
	return &session, nil
}

func (repository *memorySessions) AdvanceUploadSession(_ context.Context, session uploadsdomain.Session, received int64) (bool, error) {
	current, ok := repository.sessions[session.ID]
	if !ok || current.Received != received {
		return false, nil
	}
	repository.sessions[session.ID] = session
	return true, nil
}

func (repository *memorySessions) DeleteUploadSession(_ context.Context, sessionID string) error {
	delete(repository.sessions, sessionID)
	return nil
}

func (repository *memorySessions) ListIdleUploadSessions(_ context.Context, before time.Time) ([]string, error) {
	var ids []string
	for id, session := range repository.sessions {
		if session.UpdatedAt.Before(before) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

type memoryPartials struct {
	files     map[string][]byte
	committed map[string][]byte
}

func (partials *memoryPartials) ReservePartial(_ context.Context, sessionID string, _ int64) error {
	partials.files[sessionID] = nil
	return nil
}

func (partials *memoryPartials) AppendPartial(_ context.Context, sessionID string, offset int64, content io.Reader) (int64, error) {
	data, err := io.ReadAll(content)
	partials.files[sessionID] = append(partials.files[sessionID][:offset], data...)
	return int64(len(data)), err
}

func (partials *memoryPartials) CommitPartial(_ context.Context, sessionID, address string) (string, error) {
	partials.committed[address] = partials.files[sessionID]
	delete(partials.files, sessionID)
	return "aa/" + address, nil
}

func (partials *memoryPartials) DiscardPartial(sessionID string) error {
	delete(partials.files, sessionID)
	return nil
}

type sessionFixture struct {
	service  *UploadService
	sessions *memorySessions
	partials *memoryPartials
	roms     *testROMRepository
	uploads  *testUploadRepository
}

func newSessionFixture(storage uploadStorage) *sessionFixture {
	fixture := &sessionFixture{
		sessions: &memorySessions{sessions: map[string]uploadsdomain.Session{}},
		partials: &memoryPartials{files: map[string][]byte{}, committed: map[string][]byte{}},
		roms:     &testROMRepository{},
		uploads:  &testUploadRepository{},
	}
	fixture.service = fixture.newService(storage)
	return fixture
}

// newService builds a service over the fixture's state, as after a restart.
func (fixture *sessionFixture) newService(storage uploadStorage) *UploadService {
	return NewUploadService(fixture.roms, fixture.uploads, storage, romhash.New(), &testQueue{}, &testAudit{}, UploadPolicy{
		MaxUploadBytes: 1 << 20, AllowedExtensions: []string{"sfc"},
	}).WithSessions(fixture.sessions, fixture.partials, romhash.New())
}

var player = identity.Requester{UserID: "player", Role: identity.RoleUser}

func TestSessionUploadArrivesInPartsAndResumesAfterARestart(t *testing.T) {
	ctx := context.Background()
	fixture := newSessionFixture(&testStorage{})
	content := "Super Mario World, sent in three parts"
	session, err := fixture.service.CreateSession(ctx, player, CreateSessionInput{Filename: "smw.sfc", Size: int64(len(content))})
	if err != nil {
		t.Fatal(err)
	}
	if session.Received != 0 || session.MaxChunkSize != MaxChunkBytes {
		t.Fatalf("new session = %+v", session)
	}
	if progress, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 0, strings.NewReader(content[:10])); err != nil || progress.Received != 10 || progress.Upload != nil {
		t.Fatalf("first part = %+v, %v", progress, err)
	}
	// A part cut off midway changes nothing; the client resends it.
	if _, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 10, io.MultiReader(strings.NewReader(content[10:15]), &failingReader{})); err == nil {
		t.Fatal("a cut-off part succeeded")
	}
	if status, _ := fixture.service.SessionStatus(ctx, player, session.SessionID); status.Received != 10 {
		t.Fatalf("after a cut-off part received = %d, want 10", status.Received)
	}
	// The server restarts; hashing continues from the saved progress.
	fixture.service = fixture.newService(&testStorage{})
	if progress, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 10, strings.NewReader(content[10:20])); err != nil || progress.Received != 20 {
		t.Fatalf("second part = %+v, %v", progress, err)
	}
	done, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 20, strings.NewReader(content[20:]))
	if err != nil || done.Upload == nil || done.Received != int64(len(content)) {
		t.Fatalf("last part = %+v, %v", done, err)
	}
	want, _ := romhash.New().Hash([]byte(content))
	if len(fixture.roms.saved) != 1 || fixture.roms.saved[0].Hash != want || fixture.roms.saved[0].Size != uint64(len(content)) {
		t.Fatalf("recorded ROM file = %+v, want hash %+v", fixture.roms.saved, want)
	}
	if string(fixture.partials.committed[want.SHA256]) != content {
		t.Fatalf("committed content = %q", fixture.partials.committed[want.SHA256])
	}
	if len(fixture.uploads.jobs) != 1 || fixture.uploads.saved[0].OriginalFilename != "smw.sfc" {
		t.Fatalf("queued jobs = %+v, uploads = %+v", fixture.uploads.jobs, fixture.uploads.saved)
	}
	if len(fixture.sessions.sessions) != 0 {
		t.Fatal("the finished session was kept")
	}
}

type failingReader struct{}

func (*failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestSessionUploadRejectsMisplacedOversizedAndForeignParts(t *testing.T) {
	ctx := context.Background()
	fixture := newSessionFixture(&testStorage{})
	session, err := fixture.service.CreateSession(ctx, player, CreateSessionInput{Filename: "game.sfc", Size: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 3, strings.NewReader("abc")); !fault.IsCode(err, fault.CodeConflict) {
		t.Fatalf("part at the wrong offset = %v, want conflict", err)
	}
	if _, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 0, strings.NewReader("nine bytes")); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("part past the declared size = %v, want validation error", err)
	}
	stranger := identity.Requester{UserID: "stranger", Role: identity.RoleAdmin}
	if _, err := fixture.service.AppendToSession(ctx, stranger, session.SessionID, 0, strings.NewReader("ab")); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("another user's part = %v, want not found", err)
	}
	if err := fixture.service.CancelSession(ctx, stranger, session.SessionID); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("another user's cancel = %v, want not found", err)
	}
	// One part at a time: a part arriving while another is written is refused.
	release, err := fixture.service.sessions.claim(session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 0, strings.NewReader("ab")); !fault.IsCode(err, fault.CodeConflict) {
		t.Fatalf("concurrent part = %v, want conflict", err)
	}
	release()
	if err := fixture.service.CancelSession(ctx, player, session.SessionID); err != nil {
		t.Fatal(err)
	}
	if len(fixture.sessions.sessions) != 0 || len(fixture.partials.files) != 0 {
		t.Fatal("cancelling kept the session or its file")
	}
}

func TestSessionUploadOfADuplicateEndsTheSession(t *testing.T) {
	ctx := context.Background()
	fixture := newSessionFixture(&testStorage{})
	hash, _ := romhash.New().Hash([]byte("dup"))
	fixture.roms.existing = &romdomain.ROMFile{ID: "rf_existing", Hash: hash}
	session, _ := fixture.service.CreateSession(ctx, player, CreateSessionInput{Filename: "game.sfc", Size: 3})
	if _, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 0, strings.NewReader("dup")); !fault.IsCode(err, fault.CodeROMAlreadyExists) {
		t.Fatalf("duplicate = %v, want rom already exists", err)
	}
	if len(fixture.sessions.sessions) != 0 || len(fixture.partials.files) != 0 {
		t.Fatal("the duplicate's session or file was kept")
	}
}

func TestSessionUploadRetriesAFailedFinishWithAnEmptyPart(t *testing.T) {
	ctx := context.Background()
	fixture := newSessionFixture(&testStorage{})
	fixture.roms.upsertErr = nil
	failing := errors.New("database offline")
	fixture.uploads.err = nil
	session, _ := fixture.service.CreateSession(ctx, player, CreateSessionInput{Filename: "game.sfc", Size: 4})
	fixture.roms.existing = nil
	lookups := &flakyROMRepository{testROMRepository: fixture.roms, failures: 1, err: failing}
	fixture.service.romRepository = lookups
	if _, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 0, strings.NewReader("rom!")); !errors.Is(err, failing) {
		t.Fatalf("finish with the database offline = %v", err)
	}
	if status, err := fixture.service.SessionStatus(ctx, player, session.SessionID); err != nil || status.Received != 4 {
		t.Fatalf("session after a failed finish = %+v, %v; want it kept with every byte", status, err)
	}
	done, err := fixture.service.AppendToSession(ctx, player, session.SessionID, 4, bytes.NewReader(nil))
	if err != nil || done.Upload == nil {
		t.Fatalf("retried finish = %+v, %v", done, err)
	}
}

// flakyROMRepository fails its first lookups.
type flakyROMRepository struct {
	*testROMRepository
	failures int
	err      error
}

func (repository *flakyROMRepository) FindROMFileBySHA256(ctx context.Context, sha256 string) (*romdomain.ROMFile, error) {
	if repository.failures > 0 {
		repository.failures--
		return nil, repository.err
	}
	return repository.testROMRepository.FindROMFileBySHA256(ctx, sha256)
}

func TestSessionCreationChecksTheUploadAndExpiresIdleSessions(t *testing.T) {
	ctx := context.Background()
	fixture := newSessionFixture(&roomyStorage{room: 100})
	for _, test := range []struct {
		input CreateSessionInput
		code  fault.Code
	}{
		{CreateSessionInput{Filename: "game.exe", Size: 10}, fault.CodeUnsupportedUpload},
		{CreateSessionInput{Filename: "game.sfc", Size: 0}, fault.CodeValidation},
		{CreateSessionInput{Filename: "game.sfc", Size: 2 << 20}, fault.CodeUploadTooLarge},
		{CreateSessionInput{Filename: "game.sfc", Size: 1000}, fault.CodeInsufficientStorage},
	} {
		if _, err := fixture.service.CreateSession(ctx, player, test.input); !fault.IsCode(err, test.code) {
			t.Errorf("CreateSession(%+v) = %v, want %s", test.input, err, test.code)
		}
	}
	session, err := fixture.service.CreateSession(ctx, player, CreateSessionInput{Filename: "game.sfc", Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if expired, _ := fixture.service.ExpireSessions(ctx, time.Now().Add(-time.Hour)); expired != 0 {
		t.Fatal("an active session expired")
	}
	if expired, _ := fixture.service.ExpireSessions(ctx, time.Now().Add(time.Second)); expired != 1 {
		t.Fatal("an idle session did not expire")
	}
	if _, err := fixture.service.SessionStatus(ctx, player, session.SessionID); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("expired session = %v, want not found", err)
	}
}
