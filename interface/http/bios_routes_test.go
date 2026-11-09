package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/bios"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	uploadfeature "github.com/lcmps/retroxide/application/uploads"
	biosdomain "github.com/lcmps/retroxide/domain/bios"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// tokenAuthenticator resolves each bearer token to its own requester.
type tokenAuthenticator map[string]identity.Requester

func (authenticator tokenAuthenticator) Authenticate(_ context.Context, token string) (identity.Requester, error) {
	requester, ok := authenticator[token]
	if !ok {
		return identity.Requester{}, fault.Unauthorized()
	}
	return requester, nil
}

// biosRouteRepository is an in-memory BIOS store for the real BIOS service.
type biosRouteRepository struct{ entries map[string]biosdomain.BIOS }

func (repository *biosRouteRepository) ListBIOS(_ context.Context, platform *string) ([]biosdomain.BIOS, error) {
	var result []biosdomain.BIOS
	for _, entry := range repository.entries {
		if platform == nil || entry.Platform == *platform {
			result = append(result, entry)
		}
	}
	return result, nil
}

func (repository *biosRouteRepository) FindBIOS(_ context.Context, id string) (*biosdomain.BIOS, error) {
	if entry, ok := repository.entries[id]; ok {
		return &entry, nil
	}
	return nil, nil
}

func (repository *biosRouteRepository) ListUnrecognizedBIOS(context.Context) ([]biosdomain.BIOS, error) {
	return nil, nil
}

func (repository *biosRouteRepository) UpdateBIOS(_ context.Context, entry biosdomain.BIOS) error {
	repository.entries[entry.ID] = entry
	return nil
}

func (repository *biosRouteRepository) HardDeleteBIOS(_ context.Context, id string, _ func(string) error) error {
	delete(repository.entries, id)
	return nil
}

func (repository *biosRouteRepository) FindROMFileByID(_ context.Context, id string) (*romdomain.ROMFile, error) {
	return &romdomain.ROMFile{ID: id, StoragePath: "stored/" + id}, nil
}

type biosRouteStorage struct{}

func (biosRouteStorage) Write(context.Context, string, []byte) (string, error) { return "", nil }
func (biosRouteStorage) Read(context.Context, string) ([]byte, error)          { return nil, nil }
func (biosRouteStorage) Delete(context.Context, string) error                  { return nil }
func (biosRouteStorage) Open(context.Context, string) (rom.StoredContent, error) {
	return rom.StoredContent{ReadSeekCloser: nopSeekCloser{strings.NewReader("bios bytes")}, Size: 10}, nil
}

func newBIOSRouter(t *testing.T) (http.Handler, *biosRouteRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hash, err := romdomain.NewROMHash(strings.Repeat("a", 64), strings.Repeat("b", 40), strings.Repeat("c", 32), "0000000d")
	if err != nil {
		t.Fatal(err)
	}
	path := "pcsx2/bios/ps2-0160e-20010704.bin"
	repository := &biosRouteRepository{entries: map[string]biosdomain.BIOS{
		"bio_1": {ID: "bio_1", ROMFileID: "rf_1", UploaderID: "usr_a", Platform: "ps2", Filename: "upload.bin", ExpectedPath: &path, Recognized: true, Hash: hash, Size: 10},
	}}
	router := NewRouter(Dependencies{
		BIOS: bios.NewService(repository, biosRouteStorage{}, nil),
		Authenticator: tokenAuthenticator{
			"a":      {UserID: "usr_a", Role: identity.RoleUser},
			"b":      {UserID: "usr_b", Role: identity.RoleUser},
			"client": {UserID: "usr_a", Role: identity.RoleClient},
			"admin":  {UserID: "usr_admin", Role: identity.RoleAdmin},
		},
		DownloadLinks: linkTestSigner{},
	}, Options{})
	return router, repository
}

func sendAs(router http.Handler, token, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestBIOSRoutesPermissions(t *testing.T) {
	router, repository := newBIOSRouter(t)
	for _, token := range []string{"b", "client"} {
		if response := sendAs(router, token, http.MethodGet, "/api/v1/bios?platform=ps2", ""); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"bios_id":"bio_1"`) {
			t.Fatalf("list as %s = %d %s", token, response.Code, response.Body.String())
		}
		if response := sendAs(router, token, http.MethodGet, "/api/v1/bios/bio_1/download", ""); response.Code != http.StatusOK || response.Body.String() != "bios bytes" {
			t.Fatalf("download as %s = %d %s", token, response.Code, response.Body.String())
		}
		if response := sendAs(router, token, http.MethodDelete, "/api/v1/bios/bio_1", ""); response.Code != http.StatusForbidden {
			t.Fatalf("delete as %s = %d, want 403", token, response.Code)
		}
	}
	if response := sendAs(router, "b", http.MethodPatch, "/api/v1/bios/bio_1", `{"model":"x"}`); response.Code != http.StatusForbidden {
		t.Fatalf("label as another user = %d, want 403", response.Code)
	}
	if response := sendAs(router, "", http.MethodGet, "/api/v1/bios", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("list without a token = %d, want 401", response.Code)
	}
	if response := sendAs(router, "admin", http.MethodDelete, "/api/v1/bios/bio_1", ""); response.Code != http.StatusNoContent || len(repository.entries) != 0 {
		t.Fatalf("delete as admin = %d, entries left %d", response.Code, len(repository.entries))
	}
}

func TestBIOSDownloadLink(t *testing.T) {
	router, _ := newBIOSRouter(t)
	response := sendAs(router, "b", http.MethodPost, "/api/v1/bios/bio_1/download-link", "")
	var link downloadLinkResponse
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &link) != nil || link.Filename != "ps2-0160e-20010704.bin" {
		t.Fatalf("download link = %d %s", response.Code, response.Body.String())
	}
	download := sendAs(router, "", http.MethodGet, link.URL, "")
	if download.Code != http.StatusOK || download.Body.String() != "bios bytes" || !strings.Contains(download.Header().Get("Content-Disposition"), "ps2-0160e-20010704.bin") {
		t.Fatalf("download through the link = %d %q %s", download.Code, download.Header().Get("Content-Disposition"), download.Body.String())
	}
}

// biosCapturingUploads records the BIOS options an upload arrives with.
type biosCapturingUploads struct {
	routerTestUploads
	options *uploadfeature.BIOSOptions
}

func (uploads biosCapturingUploads) Create(_ context.Context, _ identity.Requester, input uploadfeature.UploadInput) (uploadfeature.UploadAcceptedDTO, error) {
	*uploads.options = input.BIOS
	return uploadfeature.UploadAcceptedDTO{UploadID: "upl_1", Status: "processing"}, nil
}

func TestUploadPassesBIOSOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for target, want := range map[string]uploadfeature.BIOSOptions{
		"/api/v1/upload?bios=true&platform=ps2": {Enabled: true, Platform: "ps2"},
		"/api/v1/upload":                        {},
	} {
		var options uploadfeature.BIOSOptions
		router := NewRouter(Dependencies{Uploads: biosCapturingUploads{options: &options}, Authenticator: tokenAuthenticator{"a": {UserID: "usr_a", Role: identity.RoleUser}}}, Options{MaxUploadBytes: 1024})
		body, contentType := multipartUpload(t, "file", "scph5501.bin", "bios bytes")
		request := httptest.NewRequest(http.MethodPost, target, body)
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Authorization", "Bearer a")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted || options != want {
			t.Fatalf("%s = %d, options %+v, want %+v", target, response.Code, options, want)
		}
	}
}
