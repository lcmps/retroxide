package http

import (
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/bios"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
)

// biosIDPrefix starts every BIOS ID, which lets a signed download link carry either a ROM or a BIOS.
const biosIDPrefix = "bio_"

type biosHandler struct {
	bios  bios.API
	links DownloadLinks
}

// registerBIOSRoutes mounts the shared BIOS files: anyone signed in or connected reads them; only
// their uploader or an admin labels or deletes them.
func registerBIOSRoutes(api *gin.RouterGroup, service bios.API, links DownloadLinks) {
	handler := biosHandler{bios: service, links: links}
	group := api.Group("/bios", requireService(service != nil))
	group.GET("", handler.list)
	group.GET("/:bios_id", handler.get)
	group.PATCH("/:bios_id", handler.label)
	group.DELETE("/:bios_id", handler.delete)
	group.GET("/:bios_id/download", handler.download)
	group.POST("/:bios_id/download-link", requireService(links != nil), handler.createDownloadLink)
}

func (handler biosHandler) list(c *gin.Context) {
	entries, err := handler.bios.List(c.Request.Context(), requesterFromContext(c), optionalQuery(c, "platform"))
	respondJSON(c, http.StatusOK, bios.ListDTO{Items: entries}, err)
}

func (handler biosHandler) get(c *gin.Context) {
	entry, err := handler.bios.Get(c.Request.Context(), requesterFromContext(c), c.Param("bios_id"))
	respondJSON(c, http.StatusOK, entry, err)
}

func (handler biosHandler) label(c *gin.Context) {
	var input bios.LabelInput
	if !bindJSON(c, &input) {
		return
	}
	entry, err := handler.bios.Label(c.Request.Context(), requesterFromContext(c), c.Param("bios_id"), input)
	respondJSON(c, http.StatusOK, entry, err)
}

func (handler biosHandler) delete(c *gin.Context) {
	respondNoContent(c, handler.bios.Delete(c.Request.Context(), requesterFromContext(c), c.Param("bios_id")))
}

func (handler biosHandler) download(c *gin.Context) {
	handler.stream(c, requesterFromContext(c), c.Param("bios_id"))
}

func (handler biosHandler) createDownloadLink(c *gin.Context) {
	requester, biosID := requesterFromContext(c), c.Param("bios_id")
	filename, err := handler.bios.DownloadFilename(c.Request.Context(), requester, biosID)
	if err != nil {
		writeError(c, err)
		return
	}
	token, expiresAt, err := handler.links.Sign(requester, biosID)
	if err != nil {
		writeError(c, fault.Internal(err))
		return
	}
	c.JSON(http.StatusCreated, downloadLinkResponse{
		URL:       downloadLinkPath + "?" + url.Values{"token": {token}}.Encode(),
		Filename:  filename,
		ExpiresIn: int64(time.Until(expiresAt).Round(time.Second).Seconds()),
	})
}

func (handler biosHandler) stream(c *gin.Context, requester identity.Requester, biosID string) {
	output, err := handler.bios.Download(c.Request.Context(), requester, biosID)
	if err != nil {
		writeError(c, err)
		return
	}
	if output.Content.ReadSeekCloser != nil {
		defer output.Content.Close()
	}
	writeDownload(c, output)
}
