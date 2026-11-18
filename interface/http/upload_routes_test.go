package http

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	uploadfeature "github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/domain/identity"
)

// uploadRouteService accepts every upload and remembers the sessions it was asked to start.
type uploadRouteService struct {
	routerTestUploads
	sessions   []uploadfeature.CreateSessionInput
	assembled  *uploadfeature.AssembleDiscInput
	cancelled  []string
	duplicates []string
}

func (service *uploadRouteService) CreateSession(_ context.Context, _ identity.Requester, input uploadfeature.CreateSessionInput) (uploadfeature.SessionDTO, error) {
	if input.Size <= 0 {
		return uploadfeature.SessionDTO{}, fault.Validation("size must be greater than zero")
	}
	service.sessions = append(service.sessions, input)
	return uploadfeature.SessionDTO{SessionID: "ups_1", Size: input.Size, MaxChunkSize: uploadfeature.MaxChunkBytes}, nil
}

func (service *uploadRouteService) Status(_ context.Context, _ identity.Requester, uploadID string) (uploadfeature.UploadStatusDTO, error) {
	if uploadID != "upl_1" {
		return uploadfeature.UploadStatusDTO{}, fault.NotFound()
	}
	return uploadfeature.UploadStatusDTO{UploadID: uploadID, Status: "processing"}, nil
}

func (service *uploadRouteService) AssembleDisc(_ context.Context, _ identity.Requester, input uploadfeature.AssembleDiscInput) (uploadfeature.UploadAcceptedDTO, error) {
	service.assembled = &input
	return uploadfeature.UploadAcceptedDTO{UploadID: "upl_disc", Status: "processing"}, nil
}

func (service *uploadRouteService) CancelSession(_ context.Context, _ identity.Requester, sessionID string) error {
	service.cancelled = append(service.cancelled, sessionID)
	return nil
}

func (service *uploadRouteService) CheckSessionDuplicate(_ context.Context, _ identity.Requester, _ string, sha256 string) error {
	service.duplicates = append(service.duplicates, sha256)
	if sha256 == "dup" {
		return fault.ROMAlreadyExists("rom-1", sha256)
	}
	return nil
}

func (uploadRouteService) Options() uploadfeature.UploadOptionsDTO {
	return uploadfeature.UploadOptionsDTO{DiscCompression: true}
}

func newUploadRouter(t *testing.T) (http.Handler, *uploadRouteService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service := &uploadRouteService{}
	router := NewRouter(Dependencies{Uploads: service, Authenticator: tokenAuthenticator{"a": {UserID: "usr_a", Role: identity.RoleUser}}}, Options{MaxUploadBytes: 1 << 20})
	return router, service
}

func TestUploadRoutesSessionLifecycle(t *testing.T) {
	router, service := newUploadRouter(t)
	response := sendAs(router, "a", http.MethodPost, "/api/v1/upload/session", `{"filename":"game.sfc","size":2048,"mime_type":"application/octet-stream","part":true}`)
	var session uploadfeature.SessionDTO
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &session) != nil || session.SessionID != "ups_1" || session.MaxChunkSize != uploadfeature.MaxChunkBytes {
		t.Fatalf("create session = %d %s", response.Code, response.Body.String())
	}
	if len(service.sessions) != 1 || service.sessions[0].Filename != "game.sfc" || !service.sessions[0].Part || *service.sessions[0].MIMEType != "application/octet-stream" {
		t.Fatalf("session input = %+v", service.sessions)
	}
	if response := sendAs(router, "a", http.MethodPost, "/api/v1/upload/session", `{"filename":"game.sfc","size":0}`); response.Code != http.StatusBadRequest {
		t.Fatalf("empty session = %d, want 400", response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/upload/session", bytes.NewBufferString("filename=game.sfc"))
	request.Header.Set("Authorization", "Bearer a")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form-encoded session = %d, want 415", recorder.Code)
	}

	if response := sendAs(router, "a", http.MethodPost, "/api/v1/upload/session/ups_1/check", `{"sha256":"dup"}`); response.Code != http.StatusConflict {
		t.Fatalf("duplicate check = %d %s, want 409", response.Code, response.Body.String())
	}
	if response := sendAs(router, "a", http.MethodPost, "/api/v1/upload/session/ups_1/check", `{"sha256":"fresh"}`); response.Code != http.StatusNoContent {
		t.Fatalf("fresh check = %d, want 204", response.Code)
	}
	if response := sendAs(router, "a", http.MethodDelete, "/api/v1/upload/session/ups_1", ""); response.Code != http.StatusNoContent || len(service.cancelled) != 1 {
		t.Fatalf("cancel = %d, cancelled %v", response.Code, service.cancelled)
	}
	if response := sendAs(router, "a", http.MethodPatch, "/api/v1/upload/session/ups_1", "bytes"); response.Code != http.StatusBadRequest {
		t.Fatalf("part without Upload-Offset = %d, want 400", response.Code)
	}
}

func TestUploadRoutesStatusDiscAndOptions(t *testing.T) {
	router, service := newUploadRouter(t)
	response := sendAs(router, "a", http.MethodGet, "/api/v1/upload/upl_1", "")
	var status uploadfeature.UploadStatusDTO
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &status) != nil || status.Status != "processing" {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	if response := sendAs(router, "a", http.MethodGet, "/api/v1/upload/upl_missing", ""); response.Code != http.StatusNotFound {
		t.Fatalf("missing upload = %d, want 404", response.Code)
	}
	response = sendAs(router, "a", http.MethodPost, "/api/v1/upload/disc", `{"sheet_name":"Game.cue","sheet":"FILE \"Game.bin\" BINARY","parts":["upp_1"],"compress":true}`)
	if response.Code != http.StatusAccepted || service.assembled == nil || service.assembled.SheetName != "Game.cue" || !service.assembled.Compress || len(service.assembled.Parts) != 1 {
		t.Fatalf("assemble = %d %s, recorded %+v", response.Code, response.Body.String(), service.assembled)
	}
	response = sendAs(router, "a", http.MethodGet, "/api/v1/upload/options", "")
	var options uploadfeature.UploadOptionsDTO
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &options) != nil || !options.DiscCompression {
		t.Fatalf("options = %d %s", response.Code, response.Body.String())
	}
	if response := sendAs(router, "", http.MethodGet, "/api/v1/upload/options", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("options without a token = %d, want 401", response.Code)
	}
}

func TestUploadRoutesRejectNonMultipartBodies(t *testing.T) {
	router, _ := newUploadRouter(t)
	if response := sendAs(router, "a", http.MethodPost, "/api/v1/upload", `{"file":"x"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("JSON upload = %d, want 400", response.Code)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("name", "no file here")
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/upload", &body)
	request.Header.Set("Authorization", "Bearer a")
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("multipart without a file field = %d %s, want 400", recorder.Code, recorder.Body.String())
	}
}
