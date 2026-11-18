package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
)

// clientRouteService keeps one API client, cli-1, for usr_a.
type clientRouteService struct {
	created []string
	revoked []string
}

func (clientRouteService) List(_ context.Context, requester identity.Requester) (auth.ClientListResponse, error) {
	if requester.UserID != "usr_a" {
		return auth.ClientListResponse{Item: []auth.ClientDTO{}}, nil
	}
	return auth.ClientListResponse{Item: []auth.ClientDTO{{ID: "cli-1", UserID: "usr_a", Name: "Emulator"}}}, nil
}

func (service *clientRouteService) Create(_ context.Context, _ identity.Requester, name string) (auth.CreateClientResponse, error) {
	if name == "" {
		return auth.CreateClientResponse{}, fault.Validation("name is required")
	}
	service.created = append(service.created, name)
	return auth.CreateClientResponse{ID: "cli-new", Name: name, Token: "rtx-secret"}, nil
}

func (service *clientRouteService) Revoke(_ context.Context, requester identity.Requester, id string) error {
	if id != "cli-1" || requester.UserID != "usr_a" {
		return fault.NotFound()
	}
	service.revoked = append(service.revoked, id)
	return nil
}

func newClientRouter(t *testing.T) (http.Handler, *clientRouteService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service := &clientRouteService{}
	router := NewRouter(Dependencies{Clients: service, Authenticator: tokenAuthenticator{
		"a": {UserID: "usr_a", Role: identity.RoleUser},
		"b": {UserID: "usr_b", Role: identity.RoleUser},
	}}, Options{})
	return router, service
}

func TestClientRoutes(t *testing.T) {
	router, service := newClientRouter(t)
	response := sendAs(router, "a", http.MethodGet, "/api/v1/client", "")
	var list auth.ClientListResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &list) != nil || len(list.Item) != 1 || list.Item[0].Name != "Emulator" {
		t.Fatalf("list = %d %s", response.Code, response.Body.String())
	}
	response = sendAs(router, "a", http.MethodPost, "/api/v1/client", `{"name":"Handheld"}`)
	var created auth.CreateClientResponse
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &created) != nil || created.Token != "rtx-secret" || len(service.created) != 1 {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	if response := sendAs(router, "a", http.MethodPost, "/api/v1/client", `{}`); response.Code != http.StatusBadRequest {
		t.Fatalf("create without a name = %d, want 400", response.Code)
	}
	if response := sendAs(router, "a", http.MethodDelete, "/api/v1/client/cli-1", ""); response.Code != http.StatusNoContent || len(service.revoked) != 1 {
		t.Fatalf("revoke = %d, revoked %v", response.Code, service.revoked)
	}
	if response := sendAs(router, "b", http.MethodDelete, "/api/v1/client/cli-1", ""); response.Code != http.StatusNotFound {
		t.Fatalf("revoke of another user's client = %d, want 404", response.Code)
	}
	if response := sendAs(router, "", http.MethodGet, "/api/v1/client", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("list without a token = %d, want 401", response.Code)
	}
}
