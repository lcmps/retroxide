package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
)

type sessionHandler struct {
	sessions auth.SessionAPI
}

func registerSessionRoutes(api *gin.RouterGroup, sessions auth.SessionAPI) {
	handler := sessionHandler{sessions: sessions}
	credentialLimiter := rateLimitMiddleware(newAttemptLimiter(credentialRefill, credentialBurst))

	group := api.Group("/session", requireService(sessions != nil))
	group.POST("", credentialLimiter, handler.login)
	group.POST("/refresh", credentialLimiter, handler.refresh)
	group.DELETE("", handler.logout)
}

func (handler sessionHandler) login(c *gin.Context) {
	var input auth.LoginRequest
	if !bindJSON(c, &input) {
		return
	}
	if err := auth.ValidateEmail(input.Email); err != nil {
		writeError(c, err)
		return
	}
	response, err := handler.sessions.Login(c.Request.Context(), input)
	if fault.IsCode(err, fault.CodeUnauthorized) {
		writeSpecificError(c, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return
	}
	respondJSON(c, http.StatusOK, response, err)
}

func (handler sessionHandler) refresh(c *gin.Context) {
	var input auth.RefreshRequest
	if !bindJSON(c, &input) {
		return
	}
	response, err := handler.sessions.Refresh(c.Request.Context(), input.RefreshToken)
	if fault.IsCode(err, fault.CodeUnauthorized) {
		writeSpecificError(c, http.StatusUnauthorized, "invalid_refresh_token", "refresh token is invalid or expired")
		return
	}
	respondJSON(c, http.StatusOK, response, err)
}

func (handler sessionHandler) logout(c *gin.Context) {
	respondNoContent(c, handler.sessions.Logout(c.Request.Context(), requesterFromContext(c)))
}
