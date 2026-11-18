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

// sessionRouteService signs "mario@example.com"/"secret" in and refreshes "good-refresh".
type sessionRouteService struct {
	loggedOut []string
}

func (sessionRouteService) Login(_ context.Context, input auth.LoginRequest) (auth.SessionResponse, error) {
	if input.Identifier() == "mario@example.com" && input.Password == "secret" {
		return auth.SessionResponse{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", ExpiresIn: 3600}, nil
	}
	return auth.SessionResponse{}, fault.Unauthorized()
}

func (sessionRouteService) Refresh(_ context.Context, token string) (auth.SessionResponse, error) {
	if token != "good-refresh" {
		return auth.SessionResponse{}, fault.Unauthorized()
	}
	return auth.SessionResponse{AccessToken: "access-2", RefreshToken: "refresh-2", TokenType: "Bearer", ExpiresIn: 3600}, nil
}

func (service *sessionRouteService) Logout(_ context.Context, requester identity.Requester) error {
	service.loggedOut = append(service.loggedOut, requester.UserID)
	return nil
}

func newSessionRouter(t *testing.T) (http.Handler, *sessionRouteService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service := &sessionRouteService{}
	router := NewRouter(Dependencies{Sessions: service, Authenticator: tokenAuthenticator{"a": {UserID: "usr_a", Role: identity.RoleUser}}}, Options{})
	return router, service
}

func TestSessionRoutesLogin(t *testing.T) {
	router, _ := newSessionRouter(t)
	response := sendAs(router, "", http.MethodPost, "/api/v1/session", `{"login":"mario@example.com","password":"secret"}`)
	var session auth.SessionResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &session) != nil || session.AccessToken != "access" || session.TokenType != "Bearer" {
		t.Fatalf("login = %d %s", response.Code, response.Body.String())
	}
	// The legacy email field still signs in.
	if response := sendAs(router, "", http.MethodPost, "/api/v1/session", `{"email":"mario@example.com","password":"secret"}`); response.Code != http.StatusOK {
		t.Fatalf("login by email = %d %s", response.Code, response.Body.String())
	}
	response = sendAs(router, "", http.MethodPost, "/api/v1/session", `{"login":"mario@example.com","password":"wrong"}`)
	var failure ErrorResponse
	if response.Code != http.StatusUnauthorized || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != "invalid_credentials" {
		t.Fatalf("wrong password = %d %s, want invalid_credentials", response.Code, response.Body.String())
	}
	if response := sendAs(router, "", http.MethodPost, "/api/v1/session", `{"password":"secret"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("login without an identifier = %d, want 400", response.Code)
	}
}

func TestSessionRoutesRefreshAndLogout(t *testing.T) {
	router, service := newSessionRouter(t)
	response := sendAs(router, "", http.MethodPost, "/api/v1/session/refresh", `{"refresh_token":"good-refresh"}`)
	var session auth.SessionResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &session) != nil || session.AccessToken != "access-2" {
		t.Fatalf("refresh = %d %s", response.Code, response.Body.String())
	}
	response = sendAs(router, "", http.MethodPost, "/api/v1/session/refresh", `{"refresh_token":"stale"}`)
	var failure ErrorResponse
	if response.Code != http.StatusUnauthorized || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != "invalid_refresh_token" {
		t.Fatalf("stale refresh = %d %s, want invalid_refresh_token", response.Code, response.Body.String())
	}
	if response := sendAs(router, "a", http.MethodDelete, "/api/v1/session", ""); response.Code != http.StatusNoContent || len(service.loggedOut) != 1 || service.loggedOut[0] != "usr_a" {
		t.Fatalf("logout = %d, logged out %v", response.Code, service.loggedOut)
	}
	if response := sendAs(router, "", http.MethodDelete, "/api/v1/session", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("logout without a token = %d, want 401", response.Code)
	}
}
