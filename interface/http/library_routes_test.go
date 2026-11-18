package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/libraries"
	"github.com/lcmps/retroxide/domain/identity"
)

// libraryRouteService owns one library, lib-1, belonging to usr_a.
type libraryRouteService struct {
	created []string
	items   map[string][]string
}

func (service *libraryRouteService) owned(requester identity.Requester, libraryID string) error {
	if libraryID != "lib-1" || !requester.CanAccess("usr_a") {
		return fault.NotFound()
	}
	return nil
}

func (service *libraryRouteService) List(context.Context, identity.Requester) ([]libraries.LibraryDTO, error) {
	return []libraries.LibraryDTO{{LibraryID: "lib-1", Name: "Favourites"}}, nil
}

func (service *libraryRouteService) Create(_ context.Context, _ identity.Requester, name string) (libraries.LibraryDTO, error) {
	if err := libraries.ValidateName(name); err != nil {
		return libraries.LibraryDTO{}, err
	}
	service.created = append(service.created, name)
	return libraries.LibraryDTO{LibraryID: "lib-new", Name: name}, nil
}

func (service *libraryRouteService) Get(_ context.Context, requester identity.Requester, libraryID string) (libraries.LibraryDetailDTO, error) {
	if err := service.owned(requester, libraryID); err != nil {
		return libraries.LibraryDetailDTO{}, err
	}
	return libraries.LibraryDetailDTO{LibraryID: libraryID, Name: "Favourites", ROMIDs: service.items[libraryID]}, nil
}

func (service *libraryRouteService) AddItem(_ context.Context, requester identity.Requester, libraryID, romID string) error {
	if err := service.owned(requester, libraryID); err != nil {
		return err
	}
	if service.items == nil {
		service.items = map[string][]string{}
	}
	service.items[libraryID] = append(service.items[libraryID], romID)
	return nil
}

func (service *libraryRouteService) RemoveItem(_ context.Context, requester identity.Requester, libraryID, romID string) error {
	if err := service.owned(requester, libraryID); err != nil {
		return err
	}
	for _, item := range service.items[libraryID] {
		if item == romID {
			return nil
		}
	}
	return fault.NotFound()
}

func newLibraryRouter(t *testing.T) (http.Handler, *libraryRouteService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service := &libraryRouteService{}
	router := NewRouter(Dependencies{Libraries: service, Authenticator: tokenAuthenticator{
		"a": {UserID: "usr_a", Role: identity.RoleUser},
		"b": {UserID: "usr_b", Role: identity.RoleUser},
	}}, Options{})
	return router, service
}

func TestLibraryRoutes(t *testing.T) {
	router, service := newLibraryRouter(t)
	response := sendAs(router, "a", http.MethodGet, "/api/v1/library", "")
	var list []libraries.LibraryDTO
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &list) != nil || len(list) != 1 {
		t.Fatalf("list = %d %s", response.Code, response.Body.String())
	}
	response = sendAs(router, "a", http.MethodPost, "/api/v1/library", `{"name":"RPGs"}`)
	var created libraries.LibraryDTO
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &created) != nil || created.Name != "RPGs" || len(service.created) != 1 {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	if response := sendAs(router, "a", http.MethodPost, "/api/v1/library", `{"name":""}`); response.Code != http.StatusBadRequest {
		t.Fatalf("create with an empty name = %d, want 400", response.Code)
	}

	if response := sendAs(router, "a", http.MethodPost, "/api/v1/library/lib-1/item", `{"rom_id":"rom-1"}`); response.Code != http.StatusNoContent || len(service.items["lib-1"]) != 1 {
		t.Fatalf("add item = %d, items %v", response.Code, service.items)
	}
	response = sendAs(router, "a", http.MethodGet, "/api/v1/library/lib-1", "")
	var detail libraries.LibraryDetailDTO
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &detail) != nil || len(detail.ROMIDs) != 1 {
		t.Fatalf("get = %d %s", response.Code, response.Body.String())
	}
	if response := sendAs(router, "b", http.MethodGet, "/api/v1/library/lib-1", ""); response.Code != http.StatusNotFound {
		t.Fatalf("another user's library = %d, want 404", response.Code)
	}
	if response := sendAs(router, "a", http.MethodDelete, "/api/v1/library/lib-1/item/rom-1", ""); response.Code != http.StatusNoContent {
		t.Fatalf("remove item = %d", response.Code)
	}
	if response := sendAs(router, "a", http.MethodDelete, "/api/v1/library/lib-1/item/rom-9", ""); response.Code != http.StatusNotFound {
		t.Fatalf("remove a missing item = %d, want 404", response.Code)
	}
	if response := sendAs(router, "", http.MethodGet, "/api/v1/library", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("list without a token = %d, want 401", response.Code)
	}
}
