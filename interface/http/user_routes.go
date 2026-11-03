package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/users"
)

type userHandler struct {
	users users.API
}

// registerUserRoutes mounts account management; the service allows administrators only.
func registerUserRoutes(api *gin.RouterGroup, accounts users.API) {
	handler := userHandler{users: accounts}
	group := api.Group("/users", requireService(accounts != nil))
	group.GET("", handler.list)
	group.POST("", handler.create)
	group.PATCH("/:user_id", handler.update)
	group.DELETE("/:user_id", handler.delete)
	group.DELETE("/:user_id/sessions", handler.endSessions)
}

func (handler userHandler) list(c *gin.Context) {
	list, err := handler.users.List(c.Request.Context(), requesterFromContext(c))
	respondJSON(c, http.StatusOK, list, err)
}

func (handler userHandler) create(c *gin.Context) {
	var input users.CreateRequest
	if !bindJSON(c, &input) {
		return
	}
	created, err := handler.users.Create(c.Request.Context(), requesterFromContext(c), input)
	respondJSON(c, http.StatusCreated, created, err)
}

func (handler userHandler) update(c *gin.Context) {
	var input users.UpdateRequest
	if !bindJSON(c, &input) {
		return
	}
	updated, err := handler.users.Update(c.Request.Context(), requesterFromContext(c), c.Param("user_id"), input)
	respondJSON(c, http.StatusOK, updated, err)
}

func (handler userHandler) delete(c *gin.Context) {
	deleted, err := handler.users.Delete(c.Request.Context(), requesterFromContext(c), c.Param("user_id"))
	respondJSON(c, http.StatusOK, deleted, err)
}

func (handler userHandler) endSessions(c *gin.Context) {
	respondNoContent(c, handler.users.EndSessions(c.Request.Context(), requesterFromContext(c), c.Param("user_id")))
}
