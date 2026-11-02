package http

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
)

const (
	maxCoverArtBytes   = 5 << 20
	defaultROMPageSize = 50
)

type romHandler struct {
	roms rom.API
}

func registerROMRoutes(api *gin.RouterGroup, roms rom.API) {
	handler := romHandler{roms: roms}

	group := api.Group("/rom", requireService(roms != nil))
	group.GET("", handler.list)
	group.GET("/:rom_id", handler.get)
	group.PATCH("/:rom_id", handler.update)
	group.DELETE("/:rom_id", handler.delete)
	group.GET("/:rom_id/cover", handler.cover)
	group.POST("/:rom_id/cover", handler.uploadCover)
	group.POST("/:rom_id/scrape", handler.scrape)
	group.GET("/:rom_id/media/:kind", handler.media)
	group.GET("/:rom_id/download", handler.download)
}

// romListQuery holds the optional filters of GET /rom.
type romListQuery struct {
	limit      int
	cursor     *string
	platform   *string
	identified *bool
}

func parseROMListQuery(c *gin.Context) (romListQuery, error) {
	query := romListQuery{
		limit:    defaultROMPageSize,
		cursor:   optionalQuery(c, "cursor"),
		platform: optionalQuery(c, "platform"),
	}
	if raw := c.Query("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return romListQuery{}, fault.Validation("limit must be an integer")
		}
		query.limit = limit
	}
	if raw := c.Query("identified"); raw != "" {
		identified, err := strconv.ParseBool(raw)
		if err != nil {
			return romListQuery{}, fault.Validation("identified must be a boolean")
		}
		query.identified = &identified
	}
	return query, nil
}

func (handler romHandler) list(c *gin.Context) {
	query, err := parseROMListQuery(c)
	if err != nil {
		writeError(c, err)
		return
	}
	page, err := handler.roms.List(c.Request.Context(), requesterFromContext(c), query.limit, query.cursor, query.platform, query.identified)
	respondJSON(c, http.StatusOK, page, err)
}

func (handler romHandler) get(c *gin.Context) {
	found, err := handler.roms.Get(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"))
	respondJSON(c, http.StatusOK, found, err)
}

func (handler romHandler) update(c *gin.Context) {
	var input rom.UpdateROMRequest
	if !bindJSON(c, &input) {
		return
	}
	updated, err := handler.roms.Update(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"), input)
	respondJSON(c, http.StatusOK, updated, err)
}

func (handler romHandler) delete(c *gin.Context) {
	respondNoContent(c, handler.roms.Delete(c.Request.Context(), requesterFromContext(c), c.Param("rom_id")))
}

func (handler romHandler) cover(c *gin.Context) {
	content, err := handler.roms.ReadCover(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"))
	serveImage(c, content, err)
}

func (handler romHandler) uploadCover(c *gin.Context) {
	content, err := readCoverUpload(c)
	if err != nil {
		writeError(c, err)
		return
	}
	updated, err := handler.roms.UploadCover(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"), content)
	respondJSON(c, http.StatusOK, updated, err)
}

func (handler romHandler) scrape(c *gin.Context) {
	scraped, err := handler.roms.Scrape(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"))
	respondJSON(c, http.StatusOK, scraped, err)
}

func (handler romHandler) media(c *gin.Context) {
	content, err := handler.roms.ReadMedia(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"), c.Param("kind"))
	serveImage(c, content, err)
}

func (handler romHandler) download(c *gin.Context) {
	output, err := handler.roms.Download(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"))
	if err != nil {
		writeError(c, err)
		return
	}
	defer output.Content.Close()
	writeDownload(c, output)
	if bodyWasServed(c) && output.Served != nil {
		output.Served(c.Request.Context(), int64(c.Writer.Size()))
	}
}

// readCoverUpload reads the first multipart part as a cover image of at most maxCoverArtBytes.
func readCoverUpload(c *gin.Context) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCoverArtBytes+multipartOverhead)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return nil, fault.Validation("invalid cover upload")
	}
	part, err := reader.NextPart()
	if err != nil {
		return nil, fault.Validation("cover file is required")
	}
	defer part.Close()
	content, err := io.ReadAll(io.LimitReader(part, maxCoverArtBytes+1))
	if err != nil || len(content) == 0 || len(content) > maxCoverArtBytes || imageContentType(content) == "" {
		return nil, fault.Validation("cover must be a PNG, JPEG, or WebP image up to 5 MiB")
	}
	return content, nil
}

// serveImage writes stored image bytes with their sniffed type; unrecognized content is treated as missing.
func serveImage(c *gin.Context, content []byte, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	contentType := imageContentType(content)
	if contentType == "" {
		writeError(c, fault.NotFound())
		return
	}
	c.Data(http.StatusOK, contentType, content)
}

func imageContentType(content []byte) string {
	switch {
	case len(content) >= 8 && string(content[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(content) >= 3 && content[0] == 0xff && content[1] == 0xd8 && content[2] == 0xff:
		return "image/jpeg"
	case len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}

func optionalQuery(c *gin.Context, key string) *string {
	value, present := c.GetQuery(key)
	if !present {
		return nil
	}
	return &value
}
