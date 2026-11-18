package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/settings"
	"github.com/lcmps/retroxide/domain/identity"
)

type bootstrapTestSettings struct{ settings.API }

func (bootstrapTestSettings) Appearance(context.Context) (settings.AppearanceResponse, error) {
	return settings.AppearanceResponse{Font: "ubuntu"}, nil
}

type bootstrapTestProfiles struct{ auth.ProfileAPI }

func (bootstrapTestProfiles) Profile(_ context.Context, requester identity.Requester) (auth.ProfileResponse, error) {
	return auth.ProfileResponse{UserID: requester.UserID, Email: "mario@example.com", Role: "user"}, nil
}

type bootstrapBody struct {
	SetupRequired bool                        `json:"setup_required"`
	Appearance    settings.AppearanceResponse `json:"appearance"`
	Profile       *auth.ProfileResponse       `json:"profile"`
}

func getBootstrap(t *testing.T, router http.Handler, authorization string) (int, bootstrapBody) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var body bootstrapBody
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("bootstrap body %q: %v", response.Body.String(), err)
		}
	}
	return response.Code, body
}

func TestBootstrapServesSetupAppearanceAndTheSignedInProfileInOneCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Dependencies{
		Settings:      bootstrapTestSettings{},
		Profiles:      bootstrapTestProfiles{},
		Authenticator: tokenAuthenticator{"ok": {UserID: "usr_1", Role: identity.RoleUser}},
	}, Options{})

	status, anonymous := getBootstrap(t, router, "")
	if status != http.StatusOK || anonymous.SetupRequired || anonymous.Appearance.Font != "ubuntu" || anonymous.Profile != nil {
		t.Fatalf("anonymous bootstrap = %d %+v", status, anonymous)
	}
	status, signedIn := getBootstrap(t, router, "Bearer ok")
	if status != http.StatusOK || signedIn.Profile == nil || signedIn.Profile.UserID != "usr_1" || signedIn.Appearance.Font != "ubuntu" {
		t.Fatalf("signed-in bootstrap = %d %+v", status, signedIn)
	}
	// A stale token is reported, so the page can clear it, rather than treated as anonymous.
	if status, _ := getBootstrap(t, router, "Bearer expired"); status != http.StatusUnauthorized {
		t.Fatalf("expired bearer = %d, want 401", status)
	}
}

func TestSetupRouterBootstrapSaysSetupIsRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewSetupRouter(nil, nil, nil, nil)
	status, body := getBootstrap(t, router, "")
	if status != http.StatusOK || !body.SetupRequired {
		t.Fatalf("setup bootstrap = %d %+v", status, body)
	}
}
