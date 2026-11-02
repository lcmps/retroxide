package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/libraries"
)

type libraryHandler struct {
	libraries libraries.API
}

func registerLibraryRoutes(api *gin.RouterGroup, service libraries.API) {
	handler := libraryHandler{libraries: service}

	group := api.Group("/library", requireService(service != nil))
	group.GET("", handler.list)
	group.POST("", handler.create)
	group.GET("/:library_id", handler.get)
	group.POST("/:library_id/item", handler.addItem)
	group.DELETE("/:library_id/item/:rom_id", handler.removeItem)
}

func (handler libraryHandler) list(c *gin.Context) {
	found, err := handler.libraries.List(c.Request.Context(), requesterFromContext(c))
	respondJSON(c, http.StatusOK, found, err)
}

func (handler libraryHandler) create(c *gin.Context) {
	var input libraries.CreateLibraryRequest
	if !bindJSON(c, &input) {
		return
	}
	created, err := handler.libraries.Create(c.Request.Context(), requesterFromContext(c), input.Name)
	respondJSON(c, http.StatusCreated, created, err)
}

func (handler libraryHandler) get(c *gin.Context) {
	found, err := handler.libraries.Get(c.Request.Context(), requesterFromContext(c), c.Param("library_id"))
	respondJSON(c, http.StatusOK, found, err)
}

func (handler libraryHandler) addItem(c *gin.Context) {
	var input libraries.AddLibraryItemRequest
	if !bindJSON(c, &input) {
		return
	}
	respondNoContent(c, handler.libraries.AddItem(c.Request.Context(), requesterFromContext(c), c.Param("library_id"), input.ROMID))
}

func (handler libraryHandler) removeItem(c *gin.Context) {
	respondNoContent(c, handler.libraries.RemoveItem(c.Request.Context(), requesterFromContext(c), c.Param("library_id"), c.Param("rom_id")))
}
