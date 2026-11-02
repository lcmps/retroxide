package http

import "github.com/gin-gonic/gin"

// registerRoutes mounts every versioned API resource on api.
func registerRoutes(api *gin.RouterGroup, dependencies Dependencies, options Options) {
	registerSessionRoutes(api, dependencies.Sessions)
	registerLocaleRoutes(api, dependencies.Locales)
	registerUploadRoutes(api, dependencies.Uploads, options.MaxUploadBytes)
	registerROMRoutes(api, dependencies.ROMs)
	registerLibraryRoutes(api, dependencies.Libraries)
	registerClientRoutes(api, dependencies.Clients)
}
