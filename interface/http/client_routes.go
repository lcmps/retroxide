package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
)

type clientHandler struct {
	clients auth.ClientAPI
}

func registerClientRoutes(api *gin.RouterGroup, clients auth.ClientAPI) {
	handler := clientHandler{clients: clients}

	group := api.Group("/client", requireService(clients != nil))
	group.GET("", handler.list)
	group.POST("", handler.create)
	group.DELETE("/:id", handler.revoke)
}

func (handler clientHandler) list(c *gin.Context) {
	found, err := handler.clients.List(c.Request.Context(), requesterFromContext(c))
	respondJSON(c, http.StatusOK, found, err)
}

func (handler clientHandler) create(c *gin.Context) {
	var input auth.CreateClientRequest
	if !bindJSON(c, &input) {
		return
	}
	created, err := handler.clients.Create(c.Request.Context(), requesterFromContext(c), input.Name)
	respondJSON(c, http.StatusCreated, created, err)
}

func (handler clientHandler) revoke(c *gin.Context) {
	respondNoContent(c, handler.clients.Revoke(c.Request.Context(), requesterFromContext(c), c.Param("id")))
}
