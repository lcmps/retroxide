package http

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/settings"
)

// appearancePath serves presentation settings, such as the font, without authentication so the
// sign-in page can use them.
const appearancePath = "/api/v1/appearance"

type accountHandler struct {
	profiles auth.ProfileAPI
	settings settings.API
	// locales are the interface languages with a translation catalog.
	locales map[string][]byte
}

// registerAccountRoutes mounts the signed-in user's profile and the application settings.
func registerAccountRoutes(api *gin.RouterGroup, profiles auth.ProfileAPI, settingsAPI settings.API, locales map[string][]byte) {
	handler := accountHandler{profiles: profiles, settings: settingsAPI, locales: locales}
	// Profile updates re-check the current password, so they are throttled like sign-ins.
	credentialLimiter := rateLimitMiddleware(newAttemptLimiter(credentialRefill, credentialBurst))

	api.GET("/me", requireService(profiles != nil), handler.profile)
	api.PATCH("/me", requireService(profiles != nil), credentialLimiter, handler.updateProfile)
	api.GET("/settings", requireService(settingsAPI != nil), handler.listSettings)
	api.GET(strings.TrimPrefix(appearancePath, api.BasePath()), requireService(settingsAPI != nil), handler.appearance)
	api.PATCH("/settings", requireService(settingsAPI != nil), handler.updateSettings)
}

func (handler accountHandler) profile(c *gin.Context) {
	profile, err := handler.profiles.Profile(c.Request.Context(), requesterFromContext(c))
	respondJSON(c, http.StatusOK, profile, err)
}

func (handler accountHandler) updateProfile(c *gin.Context) {
	var input auth.UpdateProfileRequest
	if !bindJSON(c, &input) {
		return
	}
	if input.Locale != nil && strings.TrimSpace(*input.Locale) != "" {
		if _, supported := handler.locales[strings.TrimSpace(*input.Locale)]; !supported {
			writeError(c, fault.Validation("locale is not supported"))
			return
		}
	}
	updated, err := handler.profiles.UpdateProfile(c.Request.Context(), requesterFromContext(c), input)
	respondJSON(c, http.StatusOK, updated, err)
}

func (handler accountHandler) appearance(c *gin.Context) {
	appearance, err := handler.settings.Appearance(c.Request.Context())
	respondJSON(c, http.StatusOK, appearance, err)
}

func (handler accountHandler) listSettings(c *gin.Context) {
	list, err := handler.settings.List(c.Request.Context(), requesterFromContext(c))
	respondJSON(c, http.StatusOK, list, err)
}

func (handler accountHandler) updateSettings(c *gin.Context) {
	var input settings.UpdateRequest
	if !bindJSON(c, &input) {
		return
	}
	list, err := handler.settings.Update(c.Request.Context(), requesterFromContext(c), input)
	respondJSON(c, http.StatusOK, list, err)
}
