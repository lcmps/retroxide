package http

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/settings"
)

// bootstrapPath answers, in one request, what the web app needs before its first paint: whether
// setup is required, the presentation settings, and the signed-in profile when the request
// carries a bearer token. A request without one passes authentication; one with a stale token
// is refused, so the page clears it.
const bootstrapPath = "/api/v1/bootstrap"

// bootstrapResponse is what GET /bootstrap returns.
type bootstrapResponse struct {
	SetupRequired bool                        `json:"setup_required"`
	Appearance    settings.AppearanceResponse `json:"appearance"`
	Profile       *auth.ProfileResponse       `json:"profile"`
}

func registerBootstrapRoute(api *gin.RouterGroup, settingsAPI settings.API, profiles auth.ProfileAPI) {
	api.GET(strings.TrimPrefix(bootstrapPath, api.BasePath()), requireService(settingsAPI != nil), func(c *gin.Context) {
		appearance, err := settingsAPI.Appearance(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		response := bootstrapResponse{Appearance: appearance}
		if requester := requesterFromContext(c); requester.UserID != "" && profiles != nil {
			profile, err := profiles.Profile(c.Request.Context(), requester)
			switch {
			case err == nil:
				response.Profile = &profile
			case fault.IsCode(err, fault.CodeNotFound), fault.IsCode(err, fault.CodeForbidden):
				// An API client has no profile; the rest of the bootstrap still applies.
			default:
				writeError(c, err)
				return
			}
		}
		c.JSON(http.StatusOK, response)
	})
}

// registerSetupBootstrapRoute serves the bootstrap while setup is required.
func registerSetupBootstrapRoute(router *gin.Engine) {
	router.GET(bootstrapPath, func(c *gin.Context) {
		c.JSON(http.StatusOK, bootstrapResponse{SetupRequired: true, Appearance: settings.AppearanceResponse{Font: settings.UIFontPixel}})
	})
}
