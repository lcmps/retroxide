package http

import "github.com/gin-gonic/gin"

// registerRoutes mounts every versioned API resource on api.
func registerRoutes(api *gin.RouterGroup, dependencies Dependencies, options Options) {
	registerSessionRoutes(api, dependencies.Sessions)
	registerLocaleRoutes(api, dependencies.Locales)
	registerUploadRoutes(api, dependencies.Uploads, options.MaxUploadBytes)
	registerROMRoutes(api, dependencies.ROMs, dependencies.BIOS, dependencies.DownloadLinks)
	registerBIOSRoutes(api, dependencies.BIOS, dependencies.DownloadLinks)
	registerShareRoutes(api, dependencies.Share)
	registerLibraryRoutes(api, dependencies.Libraries)
	registerClientRoutes(api, dependencies.Clients)
	registerAccountRoutes(api, dependencies.Profiles, dependencies.Settings, dependencies.Locales)
	registerUserRoutes(api, dependencies.Users)
}
