package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/users"
	"github.com/lcmps/retroxide/domain/identity"
)

// userRouteService serves administrators and refuses everyone else, as the real service does.
type userRouteService struct {
	created *users.CreateRequest
	updated map[string]users.UpdateRequest
	ended   []string
}

func (service *userRouteService) admin(requester identity.Requester) error {
	if !requester.IsAdmin() {
		return fault.Forbidden()
	}
	return nil
}

func (service *userRouteService) List(_ context.Context, requester identity.Requester) (users.ListResponse, error) {
	if err := service.admin(requester); err != nil {
		return users.ListResponse{}, err
	}
	return users.ListResponse{Item: []users.UserDTO{{ID: "usr_admin", Email: "admin@example.com", Role: "admin"}}}, nil
}

func (service *userRouteService) Create(_ context.Context, requester identity.Requester, request users.CreateRequest) (users.UserDTO, error) {
	if err := service.admin(requester); err != nil {
		return users.UserDTO{}, err
	}
	if request.Email == "" {
		return users.UserDTO{}, fault.Validation("email is required")
	}
	service.created = &request
	return users.UserDTO{ID: "usr_new", Email: request.Email, Role: request.Role}, nil
}

func (service *userRouteService) Update(_ context.Context, requester identity.Requester, userID string, request users.UpdateRequest) (users.UserDTO, error) {
	if err := service.admin(requester); err != nil {
		return users.UserDTO{}, err
	}
	if userID != "usr_member" {
		return users.UserDTO{}, fault.NotFound()
	}
	if service.updated == nil {
		service.updated = map[string]users.UpdateRequest{}
	}
	service.updated[userID] = request
	return users.UserDTO{ID: userID, Role: "admin"}, nil
}

func (service *userRouteService) Delete(_ context.Context, requester identity.Requester, userID string) (users.DeleteResponse, error) {
	if err := service.admin(requester); err != nil {
		return users.DeleteResponse{}, err
	}
	if userID == requester.UserID {
		return users.DeleteResponse{}, fault.Validation("you cannot delete your own account")
	}
	return users.DeleteResponse{TransferredROMs: 3}, nil
}

func (service *userRouteService) EndSessions(_ context.Context, requester identity.Requester, userID string) error {
	if err := service.admin(requester); err != nil {
		return err
	}
	service.ended = append(service.ended, userID)
	return nil
}

func newUserRouter(t *testing.T) (http.Handler, *userRouteService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service := &userRouteService{}
	router := NewRouter(Dependencies{Users: service, Authenticator: tokenAuthenticator{
		"admin":  {UserID: "usr_admin", Role: identity.RoleAdmin},
		"member": {UserID: "usr_member", Role: identity.RoleUser},
	}}, Options{})
	return router, service
}

func TestUserRoutesAreForAdministrators(t *testing.T) {
	router, _ := newUserRouter(t)
	for _, call := range []struct{ method, target, body string }{
		{http.MethodGet, "/api/v1/users", ""},
		{http.MethodPost, "/api/v1/users", `{"email":"x@example.com","password":"longenough","role":"user"}`},
		{http.MethodPatch, "/api/v1/users/usr_member", `{"role":"admin"}`},
		{http.MethodDelete, "/api/v1/users/usr_member", ""},
		{http.MethodDelete, "/api/v1/users/usr_member/sessions", ""},
	} {
		if response := sendAs(router, "member", call.method, call.target, call.body); response.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member = %d, want 403", call.method, call.target, response.Code)
		}
		if response := sendAs(router, "", call.method, call.target, call.body); response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token = %d, want 401", call.method, call.target, response.Code)
		}
	}
}

func TestUserRoutesManageAccounts(t *testing.T) {
	router, service := newUserRouter(t)
	response := sendAs(router, "admin", http.MethodGet, "/api/v1/users", "")
	var list users.ListResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &list) != nil || len(list.Item) != 1 {
		t.Fatalf("list = %d %s", response.Code, response.Body.String())
	}

	response = sendAs(router, "admin", http.MethodPost, "/api/v1/users", `{"email":"new@example.com","username":"newbie","password":"longenough","role":"user"}`)
	var created users.UserDTO
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &created) != nil || created.ID != "usr_new" || *service.created.Username != "newbie" {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	if response := sendAs(router, "admin", http.MethodPost, "/api/v1/users", `{"password":"longenough","role":"user"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("create without an email = %d, want 400", response.Code)
	}

	response = sendAs(router, "admin", http.MethodPatch, "/api/v1/users/usr_member", `{"role":"admin","password":"reset-me-now"}`)
	if response.Code != http.StatusOK || *service.updated["usr_member"].Role != "admin" || *service.updated["usr_member"].Password != "reset-me-now" {
		t.Fatalf("update = %d %s, recorded %+v", response.Code, response.Body.String(), service.updated)
	}
	if response := sendAs(router, "admin", http.MethodPatch, "/api/v1/users/usr_missing", `{"role":"admin"}`); response.Code != http.StatusNotFound {
		t.Fatalf("update of a missing user = %d, want 404", response.Code)
	}

	response = sendAs(router, "admin", http.MethodDelete, "/api/v1/users/usr_member", "")
	var deleted users.DeleteResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &deleted) != nil || deleted.TransferredROMs != 3 {
		t.Fatalf("delete = %d %s", response.Code, response.Body.String())
	}
	if response := sendAs(router, "admin", http.MethodDelete, "/api/v1/users/usr_admin", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("self-delete = %d, want 400", response.Code)
	}
	if response := sendAs(router, "admin", http.MethodDelete, "/api/v1/users/usr_member/sessions", ""); response.Code != http.StatusNoContent || len(service.ended) != 1 {
		t.Fatalf("end sessions = %d, ended %v", response.Code, service.ended)
	}
}
