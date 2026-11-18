package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/domain/identity"
)

// imageTestROMs serves one PNG cover for rom-1 to any requester it is asked for.
type imageTestROMs struct {
	rom.API
	requesters []identity.Requester
}

var testPNG = "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 16)

func (roms *imageTestROMs) OpenCover(_ context.Context, requester identity.Requester, romID string) (rom.Image, error) {
	roms.requesters = append(roms.requesters, requester)
	if romID != "rom-1" || requester.UserID == "" {
		return rom.Image{}, fault.NotFound()
	}
	return rom.Image{StoredContent: rom.StoredContent{ReadSeekCloser: nopSeekCloser{strings.NewReader(testPNG)}, Size: int64(len(testPNG))}}, nil
}

// PlatformCatalog gives the logging test an authenticated API request that succeeds.
func (roms *imageTestROMs) PlatformCatalog() []rom.PlatformDTO { return nil }

func (roms *imageTestROMs) OpenCoverThumbnail(ctx context.Context, requester identity.Requester, romID string) (rom.Image, error) {
	return roms.OpenCover(ctx, requester, romID)
}

func (roms *imageTestROMs) OpenMedia(ctx context.Context, requester identity.Requester, romID, _ string) (rom.Image, error) {
	return roms.OpenCover(ctx, requester, romID)
}

// imageTestLinks accepts "img:<userID>:<romID>" tokens.
type imageTestLinks struct{}

func (imageTestLinks) Verify(_ context.Context, token string) (identity.Requester, string, error) {
	rest, ok := strings.CutPrefix(token, "img:")
	userID, romID, found := strings.Cut(rest, ":")
	if !ok || !found {
		return identity.Requester{}, "", fault.Unauthorized()
	}
	return identity.Requester{UserID: userID, Role: identity.RoleUser}, romID, nil
}

func newImageRouter(t *testing.T) (*gin.Engine, *imageTestROMs) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	roms := &imageTestROMs{}
	router := NewRouter(Dependencies{
		ROMs:          roms,
		ImageLinks:    imageTestLinks{},
		Authenticator: routerTestAuthenticator{requester: identity.Requester{UserID: "bearer-user", Role: identity.RoleUser}},
	}, Options{})
	return router, roms
}

func getImage(router http.Handler, path, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestImageRoutesAcceptSignedToken(t *testing.T) {
	router, roms := newImageRouter(t)
	for _, path := range []string{"/api/v1/rom/rom-1/cover", "/api/v1/rom/rom-1/cover/thumb", "/api/v1/rom/rom-1/media/title_screen"} {
		response := getImage(router, path+"?v=0123456789abcdef&token=img:user-7:rom-1", "")
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s with token = %d %q %s", path, response.Code, response.Header().Get("Content-Type"), response.Body.String())
		}
	}
	for _, requester := range roms.requesters {
		if requester.UserID != "user-7" {
			t.Fatalf("image served as %+v, want the token's user", requester)
		}
	}
}

func TestImageRoutesRefuseTokenForAnotherROM(t *testing.T) {
	router, roms := newImageRouter(t)
	response := getImage(router, "/api/v1/rom/rom-2/cover?token=img:user-7:rom-1", "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("token for another ROM = %d, want 401", response.Code)
	}
	if len(roms.requesters) != 0 {
		t.Fatal("the service was asked for an image the token does not cover")
	}
}

func TestImageRoutesStillAcceptBearer(t *testing.T) {
	router, roms := newImageRouter(t)
	response := getImage(router, "/api/v1/rom/rom-1/cover", "Bearer access")
	if response.Code != http.StatusOK {
		t.Fatalf("bearer cover = %d %s", response.Code, response.Body.String())
	}
	if len(roms.requesters) != 1 || roms.requesters[0].UserID != "bearer-user" {
		t.Fatalf("served as %+v, want the bearer's user", roms.requesters)
	}
	// A bad bearer is refused even when a valid token rides along.
	rejecting := NewRouter(Dependencies{ROMs: roms, ImageLinks: imageTestLinks{}, Authenticator: rejectingAuthenticator}, Options{})
	if response := getImage(rejecting, "/api/v1/rom/rom-1/cover?token=img:user-7:rom-1", "Bearer expired"); response.Code != http.StatusUnauthorized {
		t.Fatalf("expired bearer with token = %d, want 401", response.Code)
	}
}

func TestImageRoutesWithoutCredentialsAre401(t *testing.T) {
	router, _ := newImageRouter(t)
	for _, path := range []string{"/api/v1/rom/rom-1/cover", "/api/v1/rom/rom-1/cover?token=garbage"} {
		if response := getImage(router, path, ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("%s = %d, want 401", path, response.Code)
		}
	}
	// Other ROM routes stay strictly bearer-only.
	if response := getImage(router, "/api/v1/rom/rom-1?token=img:user-7:rom-1", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("detail with token = %d, want 401", response.Code)
	}
}
