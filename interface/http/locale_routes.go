package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
)

// registerLocaleRoutes serves the embedded translation catalogs, keyed by locale tag.
func registerLocaleRoutes(api *gin.RouterGroup, catalogs map[string][]byte) {
	api.GET("/i18n/:locale", func(c *gin.Context) {
		catalog, ok := catalogs[c.Param("locale")]
		if !ok {
			writeError(c, fault.NotFound())
			return
		}
		c.Header("Cache-Control", "public, max-age=3600")
		c.Data(http.StatusOK, "application/json", catalog)
	})
}
