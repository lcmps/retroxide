package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/share"
)

type shareHandler struct {
	share share.API
}

// registerShareRoutes mounts share links: creating one for a ROM, and previewing and redeeming
// one. Tokens are rate limited like credentials, so they cannot be guessed at speed.
func registerShareRoutes(api *gin.RouterGroup, service share.API) {
	handler := shareHandler{share: service}
	available := requireService(service != nil)
	api.POST("/rom/:rom_id/share", available, handler.create)

	limiter := rateLimitMiddleware(newAttemptLimiter(credentialRefill, credentialBurst))
	group := api.Group("/share", available, limiter)
	// Previews never spend a link, so link previews in chat apps are harmless.
	group.GET("/:token", handler.preview)
	group.GET("/:token/cover", handler.cover)
	group.POST("/:token", handler.redeem)
}

func (handler shareHandler) create(c *gin.Context) {
	link, err := handler.share.Create(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"))
	respondJSON(c, http.StatusCreated, link, err)
}

func (handler shareHandler) preview(c *gin.Context) {
	preview, err := handler.share.Preview(c.Request.Context(), requesterFromContext(c), c.Param("token"))
	respondJSON(c, http.StatusOK, preview, err)
}

func (handler shareHandler) cover(c *gin.Context) {
	content, err := handler.share.Cover(c.Request.Context(), requesterFromContext(c), c.Param("token"))
	serveImage(c, rom.Image{StoredContent: content}, err)
}

func (handler shareHandler) redeem(c *gin.Context) {
	redeemed, err := handler.share.Redeem(c.Request.Context(), requesterFromContext(c), c.Param("token"))
	respondJSON(c, http.StatusCreated, redeemed, err)
}
