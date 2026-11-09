package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/share"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// shareRouteRepository is an in-memory store for the real share service.
type shareRouteRepository struct {
	links map[string]string
	owned map[string]bool
}

func (repository *shareRouteRepository) FindROMByID(_ context.Context, id string) (*romdomain.ROM, error) {
	if id != "rom_game" {
		return nil, nil
	}
	cover := "covers/game.png"
	return &romdomain.ROM{ID: id, ROMFileID: "rf_game", UploaderID: "usr_a", Identity: romdomain.ROMIdentity{Title: "Game", Platform: "snes", CoverArtPath: &cover}}, nil
}

func (repository *shareRouteRepository) FindROMDiscs(_ context.Context, romID string) ([]romdomain.ROMDisc, error) {
	return []romdomain.ROMDisc{{ROMID: romID, ROMFileID: "rf_game", Number: 1}}, nil
}

func (repository *shareRouteRepository) CreateShareLink(_ context.Context, tokenHash, romID, _ string) error {
	repository.links[tokenHash] = romID
	return nil
}

func (repository *shareRouteRepository) FindUnusedShareLink(_ context.Context, tokenHash string) (string, bool, error) {
	romID, ok := repository.links[tokenHash]
	return romID, ok, nil
}

func (repository *shareRouteRepository) UserHasROMFile(_ context.Context, userID, romFileID string) (bool, error) {
	return repository.owned[userID+":"+romFileID], nil
}

func (repository *shareRouteRepository) RedeemShareLink(_ context.Context, tokenHash, userID, newROMID string) (string, error) {
	if _, ok := repository.links[tokenHash]; !ok {
		return "", fault.NotFound()
	}
	if repository.owned[userID+":rf_game"] {
		return "", fault.AlreadyInLibrary()
	}
	delete(repository.links, tokenHash)
	return newROMID, nil
}

// pngHeader is enough of a PNG for the image server to recognize it.
const pngHeader = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"

type shareRouteStorage struct{ biosRouteStorage }

func (shareRouteStorage) Open(context.Context, string) (rom.StoredContent, error) {
	return rom.StoredContent{ReadSeekCloser: nopSeekCloser{strings.NewReader(pngHeader)}, Size: int64(len(pngHeader))}, nil
}

func newShareRouter(t *testing.T) (http.Handler, *shareRouteRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repository := &shareRouteRepository{links: map[string]string{}, owned: map[string]bool{}}
	router := NewRouter(Dependencies{
		Share: share.NewService(repository, shareRouteStorage{}),
		Authenticator: tokenAuthenticator{
			"a":      {UserID: "usr_a", Role: identity.RoleUser},
			"b":      {UserID: "usr_b", Role: identity.RoleUser},
			"client": {UserID: "usr_a", Role: identity.RoleClient},
		},
	}, Options{})
	return router, repository
}

// shareToken creates a link to rom_game as alice and returns its token.
func shareToken(t *testing.T, router http.Handler) string {
	t.Helper()
	response := sendAs(router, "a", http.MethodPost, "/api/v1/rom/rom_game/share", "")
	var link share.LinkDTO
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &link) != nil || !strings.HasPrefix(link.URL, "/share/") {
		t.Fatalf("create link = %d %s", response.Code, response.Body.String())
	}
	return strings.TrimPrefix(link.URL, "/share/")
}

func TestShareRoutesPermissions(t *testing.T) {
	router, _ := newShareRouter(t)
	if response := sendAs(router, "client", http.MethodPost, "/api/v1/rom/rom_game/share", ""); response.Code != http.StatusForbidden {
		t.Fatalf("client create = %d, want 403", response.Code)
	}
	if response := sendAs(router, "b", http.MethodPost, "/api/v1/rom/rom_game/share", ""); response.Code != http.StatusNotFound {
		t.Fatalf("another user's create = %d, want 404", response.Code)
	}
	token := shareToken(t, router)
	if response := sendAs(router, "client", http.MethodPost, "/api/v1/share/"+token, ""); response.Code != http.StatusForbidden {
		t.Fatalf("client redeem = %d, want 403", response.Code)
	}
	if response := sendAs(router, "", http.MethodGet, "/api/v1/share/"+token, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("signed-out preview = %d, want 401", response.Code)
	}
}

func TestSharePreviewDoesNotSpend(t *testing.T) {
	router, _ := newShareRouter(t)
	token := shareToken(t, router)
	for range 2 {
		if response := sendAs(router, "b", http.MethodGet, "/api/v1/share/"+token, ""); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title":"Game"`) {
			t.Fatalf("preview = %d %s", response.Code, response.Body.String())
		}
	}
	if response := sendAs(router, "b", http.MethodGet, "/api/v1/share/"+token+"/cover", ""); response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("cover = %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	if response := sendAs(router, "b", http.MethodPost, "/api/v1/share/"+token, ""); response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"rom_id":"rom_`) {
		t.Fatalf("redeem = %d %s", response.Code, response.Body.String())
	}
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		if response := sendAs(router, "b", method, "/api/v1/share/"+token, ""); response.Code != http.StatusNotFound {
			t.Fatalf("%s after redeeming = %d, want 404", method, response.Code)
		}
	}
}

func TestShareRedeemAlreadyInLibrary(t *testing.T) {
	router, repository := newShareRouter(t)
	token := shareToken(t, router)
	repository.owned["usr_b:rf_game"] = true
	response := sendAs(router, "b", http.MethodPost, "/api/v1/share/"+token, "")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"already_in_library"`) {
		t.Fatalf("redeem of an owned ROM = %d %s, want 409 already_in_library", response.Code, response.Body.String())
	}
	if len(repository.links) != 1 {
		t.Fatal("a refused redeem spent the link")
	}
}
