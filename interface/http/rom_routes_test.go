package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/domain/identity"
)

// romRouteService holds one ROM, rom-1, owned by usr_a; it records what it was asked.
type romRouteService struct {
	rom.API
	updated *rom.UpdateROMRequest
	deleted []string
	listed  *rom.ListRequest
}

func (service *romRouteService) detail(requester identity.Requester, romID string) (rom.ROMDetailDTO, error) {
	if romID != "rom-1" || !requester.CanAccess("usr_a") {
		return rom.ROMDetailDTO{}, fault.NotFound()
	}
	return rom.ROMDetailDTO{ROMID: "rom-1", Title: "Game", Platform: "snes", PlatformName: "SNES"}, nil
}

func (service *romRouteService) Get(_ context.Context, requester identity.Requester, romID string) (rom.ROMDetailDTO, error) {
	return service.detail(requester, romID)
}

func (service *romRouteService) Update(_ context.Context, requester identity.Requester, romID string, input rom.UpdateROMRequest) (rom.ROMDetailDTO, error) {
	if input.Title == "" {
		return rom.ROMDetailDTO{}, fault.Validation("title is required")
	}
	service.updated = &input
	return service.detail(requester, romID)
}

func (service *romRouteService) Delete(_ context.Context, requester identity.Requester, romID string) error {
	if _, err := service.detail(requester, romID); err != nil {
		return err
	}
	service.deleted = append(service.deleted, romID)
	return nil
}

func (service *romRouteService) List(_ context.Context, _ identity.Requester, request rom.ListRequest) (rom.ROMListResponseDTO, error) {
	if request.Limit <= 0 {
		return rom.ROMListResponseDTO{}, fault.Validation("limit must be greater than zero")
	}
	service.listed = &request
	return rom.ROMListResponseDTO{Item: []rom.ROMSummaryDTO{{ROMID: "rom-1", Title: "Game"}}}, nil
}

func (service *romRouteService) Platforms(context.Context, identity.Requester) ([]rom.PlatformDTO, error) {
	return []rom.PlatformDTO{{Key: "snes", Name: "SNES", ROMCount: 1}}, nil
}

func (service *romRouteService) PlatformCatalog() []rom.PlatformDTO { return nil }

func newROMRouter(t *testing.T) (http.Handler, *romRouteService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service := &romRouteService{}
	router := NewRouter(Dependencies{ROMs: service, Authenticator: tokenAuthenticator{
		"a": {UserID: "usr_a", Role: identity.RoleUser},
		"b": {UserID: "usr_b", Role: identity.RoleUser},
	}}, Options{})
	return router, service
}

func TestROMRoutesListParsesTheQuery(t *testing.T) {
	router, service := newROMRouter(t)
	response := sendAs(router, "a", http.MethodGet, "/api/v1/rom?limit=5&platform=snes&q=mario&sort=title&order=desc&cursor=c1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list = %d %s", response.Code, response.Body.String())
	}
	var page rom.ROMListResponseDTO
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Item) != 1 || page.Item[0].ROMID != "rom-1" {
		t.Fatalf("list body = %s (%v)", response.Body.String(), err)
	}
	request := service.listed
	if request == nil || request.Limit != 5 || *request.Platform != "snes" || *request.Search != "mario" || request.Sort != "title" || request.Order != "desc" || *request.Cursor != "c1" {
		t.Fatalf("parsed request = %+v", request)
	}
	for _, query := range []string{"limit=x", "limit=-1", "identified=maybe"} {
		if response := sendAs(router, "a", http.MethodGet, "/api/v1/rom?"+query, ""); response.Code != http.StatusBadRequest {
			t.Errorf("list with %s = %d, want 400", query, response.Code)
		}
	}
	if response := sendAs(router, "a", http.MethodGet, "/api/v1/rom?limit=50", ""); response.Code != http.StatusOK || service.listed.Limit != 50 {
		t.Fatalf("default page = %d, limit %d", response.Code, service.listed.Limit)
	}
}

func TestROMRoutesGetUpdateAndDelete(t *testing.T) {
	router, service := newROMRouter(t)
	response := sendAs(router, "a", http.MethodGet, "/api/v1/rom/rom-1", "")
	var detail rom.ROMDetailDTO
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &detail) != nil || detail.PlatformName != "SNES" {
		t.Fatalf("get = %d %s", response.Code, response.Body.String())
	}
	if response := sendAs(router, "b", http.MethodGet, "/api/v1/rom/rom-1", ""); response.Code != http.StatusNotFound {
		t.Fatalf("another user's ROM = %d, want 404", response.Code)
	}

	response = sendAs(router, "a", http.MethodPatch, "/api/v1/rom/rom-1", `{"title":"Game 2","platform":"snes","genre":"RPG"}`)
	if response.Code != http.StatusOK || service.updated == nil || service.updated.Title != "Game 2" || *service.updated.Genre != "RPG" {
		t.Fatalf("update = %d %s, recorded %+v", response.Code, response.Body.String(), service.updated)
	}
	if response := sendAs(router, "a", http.MethodPatch, "/api/v1/rom/rom-1", `{"platform":"snes"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("update without a title = %d, want 400", response.Code)
	}
	if response := sendAs(router, "a", http.MethodPatch, "/api/v1/rom/rom-1", `{"title":`); response.Code != http.StatusBadRequest {
		t.Fatalf("update with broken JSON = %d, want 400", response.Code)
	}

	if response := sendAs(router, "a", http.MethodDelete, "/api/v1/rom/rom-1", ""); response.Code != http.StatusNoContent || len(service.deleted) != 1 {
		t.Fatalf("delete = %d, deleted %v", response.Code, service.deleted)
	}
	if response := sendAs(router, "b", http.MethodDelete, "/api/v1/rom/rom-1", ""); response.Code != http.StatusNotFound {
		t.Fatalf("delete of another user's ROM = %d, want 404", response.Code)
	}
}

func TestROMRoutesPlatformsAndAuthentication(t *testing.T) {
	router, _ := newROMRouter(t)
	response := sendAs(router, "a", http.MethodGet, "/api/v1/platforms", "")
	var platforms struct {
		Item []rom.PlatformDTO `json:"item"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &platforms) != nil || len(platforms.Item) != 1 || platforms.Item[0].Key != "snes" {
		t.Fatalf("platforms = %d %s", response.Code, response.Body.String())
	}
	for _, target := range []string{"/api/v1/rom", "/api/v1/rom/rom-1", "/api/v1/platforms"} {
		if response := sendAs(router, "", http.MethodGet, target, ""); response.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token = %d, want 401", target, response.Code)
		}
	}
}
