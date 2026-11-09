package share

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

type fakeRepository struct {
	roms     map[string]romdomain.ROM
	links    map[string]string // token hash → rom ID, while unused
	owned    map[string]bool   // user + ":" + rom file ID
	redeemed []string          // "hash user newID"
}

func (repository *fakeRepository) FindROMByID(_ context.Context, id string) (*romdomain.ROM, error) {
	if found, ok := repository.roms[id]; ok {
		return &found, nil
	}
	return nil, nil
}

func (repository *fakeRepository) FindROMDiscs(_ context.Context, romID string) ([]romdomain.ROMDisc, error) {
	found := repository.roms[romID]
	return []romdomain.ROMDisc{{ROMID: romID, ROMFileID: found.ROMFileID, Number: 1}, {ROMID: romID, ROMFileID: "rf_disc2", Number: 2}}, nil
}

func (repository *fakeRepository) CreateShareLink(_ context.Context, tokenHash, romID, _ string) error {
	repository.links[tokenHash] = romID
	return nil
}

func (repository *fakeRepository) FindUnusedShareLink(_ context.Context, tokenHash string) (string, bool, error) {
	romID, ok := repository.links[tokenHash]
	return romID, ok, nil
}

func (repository *fakeRepository) UserHasROMFile(_ context.Context, userID, romFileID string) (bool, error) {
	return repository.owned[userID+":"+romFileID], nil
}

func (repository *fakeRepository) RedeemShareLink(_ context.Context, tokenHash, userID, newROMID string) (string, error) {
	if _, ok := repository.links[tokenHash]; !ok {
		return "", fault.NotFound()
	}
	delete(repository.links, tokenHash)
	repository.redeemed = append(repository.redeemed, tokenHash+" "+userID+" "+newROMID)
	return newROMID, nil
}

type fakeStorage struct{ opened string }

func (storage *fakeStorage) Write(context.Context, string, []byte) (string, error) { return "", nil }
func (storage *fakeStorage) Read(context.Context, string) ([]byte, error)          { return nil, nil }
func (storage *fakeStorage) Delete(context.Context, string) error                  { return nil }
func (storage *fakeStorage) Open(_ context.Context, path string) (rom.StoredContent, error) {
	storage.opened = path
	return rom.StoredContent{}, nil
}

var (
	alice  = identity.Requester{UserID: "usr_alice", Role: identity.RoleUser}
	bob    = identity.Requester{UserID: "usr_bob", Role: identity.RoleUser}
	admin  = identity.Requester{UserID: "usr_admin", Role: identity.RoleAdmin}
	client = identity.Requester{UserID: "usr_alice", Role: identity.RoleClient}
)

func newTestService() (*Service, *fakeRepository, *fakeStorage) {
	thumb := "thumbs/game.png"
	repository := &fakeRepository{
		roms: map[string]romdomain.ROM{"rom_game": {ID: "rom_game", ROMFileID: "rf_disc1", UploaderID: "usr_alice",
			Identity: romdomain.ROMIdentity{Title: "Final Fantasy VII", Platform: "psx", CoverThumbPath: &thumb}}},
		links: map[string]string{},
		owned: map[string]bool{},
	}
	storage := &fakeStorage{}
	return NewService(repository, storage), repository, storage
}

func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tokenOf(t *testing.T, link LinkDTO) string {
	t.Helper()
	token, ok := strings.CutPrefix(link.URL, "/share/")
	if !ok || len(token) != 43 {
		t.Fatalf("link URL = %q, want /share/ and a 43-character token", link.URL)
	}
	return token
}

func TestOwnerAndAdminCreateLinks(t *testing.T) {
	ctx := context.Background()
	service, repository, _ := newTestService()
	for _, requester := range []identity.Requester{alice, admin} {
		link, err := service.Create(ctx, requester, "rom_game")
		if err != nil {
			t.Fatalf("Create as %+v = %v", requester, err)
		}
		if repository.links[hashOf(tokenOf(t, link))] != "rom_game" {
			t.Fatalf("stored link is not keyed by the token's SHA-256: %v", repository.links)
		}
	}
	if len(repository.links) != 2 {
		t.Fatalf("%d links stored, want a new one per call", len(repository.links))
	}
	if _, err := service.Create(ctx, bob, "rom_game"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("Create by another user = %v, want not found", err)
	}
	if _, err := service.Create(ctx, alice, "rom_missing"); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("Create for a missing ROM = %v, want not found", err)
	}
}

func TestClientsCannotUseShareLinks(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService()
	link, _ := service.Create(ctx, alice, "rom_game")
	token := tokenOf(t, link)
	if _, err := service.Create(ctx, client, "rom_game"); !fault.IsCode(err, fault.CodeForbidden) {
		t.Errorf("client Create = %v", err)
	}
	if _, err := service.Preview(ctx, client, token); !fault.IsCode(err, fault.CodeForbidden) {
		t.Errorf("client Preview = %v", err)
	}
	if _, err := service.Cover(ctx, client, token); !fault.IsCode(err, fault.CodeForbidden) {
		t.Errorf("client Cover = %v", err)
	}
	if _, err := service.Redeem(ctx, client, token); !fault.IsCode(err, fault.CodeForbidden) {
		t.Errorf("client Redeem = %v", err)
	}
}

func TestPreviewCoverAndRedeem(t *testing.T) {
	ctx := context.Background()
	service, repository, storage := newTestService()
	link, _ := service.Create(ctx, alice, "rom_game")
	token := tokenOf(t, link)

	preview, err := service.Preview(ctx, bob, token)
	if err != nil || preview.Title != "Final Fantasy VII" || preview.PlatformName != "PlayStation" || preview.DiscCount != 2 || !preview.HasCover || preview.AlreadyInLibrary {
		t.Fatalf("Preview = %+v, %v", preview, err)
	}
	repository.owned["usr_bob:rf_disc1"] = true
	if preview, _ := service.Preview(ctx, bob, token); !preview.AlreadyInLibrary {
		t.Fatal("Preview does not say bob already has the ROM")
	}
	delete(repository.owned, "usr_bob:rf_disc1")

	if _, err := service.Cover(ctx, bob, token); err != nil || storage.opened != "thumbs/game.png" {
		t.Fatalf("Cover = %v, opened %q", err, storage.opened)
	}
	redeemed, err := service.Redeem(ctx, bob, token)
	if err != nil || !strings.HasPrefix(redeemed.ROMID, "rom_") || len(repository.redeemed) != 1 || repository.redeemed[0] != hashOf(token)+" usr_bob "+redeemed.ROMID {
		t.Fatalf("Redeem = %+v, %v, recorded %v", redeemed, err, repository.redeemed)
	}
	for name, call := range map[string]func() error{
		"Preview": func() error { _, err := service.Preview(ctx, bob, token); return err },
		"Cover":   func() error { _, err := service.Cover(ctx, bob, token); return err },
		"Redeem":  func() error { _, err := service.Redeem(ctx, bob, token); return err },
	} {
		if err := call(); !fault.IsCode(err, fault.CodeNotFound) {
			t.Errorf("%s of a used link = %v, want not found", name, err)
		}
	}
}
