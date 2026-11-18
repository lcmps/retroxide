package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/settings"
	"github.com/lcmps/retroxide/domain/identity"
)

type accountRouteProfiles struct {
	updated *auth.UpdateProfileRequest
}

func (accountRouteProfiles) Profile(_ context.Context, requester identity.Requester) (auth.ProfileResponse, error) {
	if requester.Role == identity.RoleClient {
		return auth.ProfileResponse{}, fault.NotFound()
	}
	return auth.ProfileResponse{UserID: requester.UserID, Email: "mario@example.com", Role: "user"}, nil
}

func (profiles *accountRouteProfiles) UpdateProfile(_ context.Context, requester identity.Requester, request auth.UpdateProfileRequest) (auth.UpdateProfileResponse, error) {
	profiles.updated = &request
	return auth.UpdateProfileResponse{Profile: auth.ProfileResponse{UserID: requester.UserID, Email: "mario@example.com", Role: "user", Locale: request.Locale}}, nil
}

type accountRouteSettings struct {
	updated map[string]string
}

func (accountRouteSettings) List(_ context.Context, requester identity.Requester) (settings.ListResponse, error) {
	return settings.ListResponse{Item: []settings.SettingDTO{{Key: "RETROXIDE_UI_FONT", Value: "pixel"}}, CanEdit: requester.IsAdmin()}, nil
}

func (service *accountRouteSettings) Update(_ context.Context, requester identity.Requester, request settings.UpdateRequest) (settings.ListResponse, error) {
	if !requester.IsAdmin() {
		return settings.ListResponse{}, fault.Forbidden()
	}
	service.updated = request.Values
	return settings.ListResponse{CanEdit: true, RestartRequired: true}, nil
}

func (accountRouteSettings) Appearance(context.Context) (settings.AppearanceResponse, error) {
	return settings.AppearanceResponse{Font: "ubuntu"}, nil
}

func newAccountRouter(t *testing.T) (http.Handler, *accountRouteProfiles, *accountRouteSettings) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	profiles, settingsAPI := &accountRouteProfiles{}, &accountRouteSettings{}
	router := NewRouter(Dependencies{
		Profiles: profiles, Settings: settingsAPI, Locales: map[string][]byte{"en": []byte("{}"), "pt-BR": []byte("{}")},
		Authenticator: tokenAuthenticator{
			"user":   {UserID: "usr_1", Role: identity.RoleUser},
			"admin":  {UserID: "usr_admin", Role: identity.RoleAdmin},
			"client": {UserID: "usr_1", Role: identity.RoleClient},
		},
	}, Options{})
	return router, profiles, settingsAPI
}

func TestAccountRoutesProfile(t *testing.T) {
	router, profiles, _ := newAccountRouter(t)
	response := sendAs(router, "user", http.MethodGet, "/api/v1/me", "")
	var profile auth.ProfileResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &profile) != nil || profile.UserID != "usr_1" {
		t.Fatalf("profile = %d %s", response.Code, response.Body.String())
	}
	if response := sendAs(router, "client", http.MethodGet, "/api/v1/me", ""); response.Code != http.StatusNotFound {
		t.Fatalf("client profile = %d, want 404", response.Code)
	}
	if response := sendAs(router, "", http.MethodGet, "/api/v1/me", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("profile without a token = %d, want 401", response.Code)
	}

	response = sendAs(router, "user", http.MethodPatch, "/api/v1/me", `{"locale":"pt-BR","font":"ubuntu"}`)
	if response.Code != http.StatusOK || profiles.updated == nil || *profiles.updated.Locale != "pt-BR" {
		t.Fatalf("update = %d %s, recorded %+v", response.Code, response.Body.String(), profiles.updated)
	}
	if response := sendAs(router, "user", http.MethodPatch, "/api/v1/me", `{"locale":"xx"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported locale = %d, want 400", response.Code)
	}
	if response := sendAs(router, "user", http.MethodPatch, "/api/v1/me", `not json`); response.Code != http.StatusBadRequest {
		t.Fatalf("broken JSON = %d, want 400", response.Code)
	}
}

func TestAccountRoutesSettingsAndAppearance(t *testing.T) {
	router, _, settingsAPI := newAccountRouter(t)
	response := sendAs(router, "admin", http.MethodGet, "/api/v1/settings", "")
	var list settings.ListResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &list) != nil || !list.CanEdit || len(list.Item) != 1 {
		t.Fatalf("settings = %d %s", response.Code, response.Body.String())
	}
	response = sendAs(router, "admin", http.MethodPatch, "/api/v1/settings", `{"values":{"RETROXIDE_UI_FONT":"system"}}`)
	if response.Code != http.StatusOK || settingsAPI.updated["RETROXIDE_UI_FONT"] != "system" {
		t.Fatalf("update settings = %d %s, recorded %v", response.Code, response.Body.String(), settingsAPI.updated)
	}
	if response := sendAs(router, "user", http.MethodPatch, "/api/v1/settings", `{"values":{"RETROXIDE_UI_FONT":"system"}}`); response.Code != http.StatusForbidden {
		t.Fatalf("update settings as a user = %d, want 403", response.Code)
	}
	// The appearance is public, so the sign-in page can show the right font.
	response = sendAs(router, "", http.MethodGet, "/api/v1/appearance", "")
	var appearance settings.AppearanceResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &appearance) != nil || appearance.Font != "ubuntu" {
		t.Fatalf("appearance = %d %s", response.Code, response.Body.String())
	}
}
