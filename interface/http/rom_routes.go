package http

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/domain/identity"
)

const (
	maxCoverArtBytes   = 5 << 20
	defaultROMPageSize = 50
)

// downloadLinkPath serves downloads authorized by a signed link token rather than a bearer token.
const downloadLinkPath = "/api/v1/download"

type romHandler struct {
	roms  rom.API
	links DownloadLinks
}

func registerROMRoutes(api *gin.RouterGroup, roms rom.API, links DownloadLinks) {
	handler := romHandler{roms: roms, links: links}
	api.GET(strings.TrimPrefix(downloadLinkPath, api.BasePath()), requireService(roms != nil && links != nil), handler.downloadWithLink)

	api.GET("/platforms", requireService(roms != nil), handler.platforms)
	api.GET("/platforms/catalog", requireService(roms != nil), handler.platformCatalog)

	group := api.Group("/rom", requireService(roms != nil))
	group.GET("", handler.list)
	group.GET("/:rom_id", handler.get)
	group.PATCH("/:rom_id", handler.update)
	group.DELETE("/:rom_id", handler.delete)
	group.GET("/:rom_id/cover", handler.cover)
	group.GET("/:rom_id/cover/thumb", handler.coverThumbnail)
	group.POST("/:rom_id/cover", handler.uploadCover)
	group.POST("/:rom_id/scrape", handler.scrape)
	group.POST("/:rom_id/compress", handler.compress)
	group.POST("/:rom_id/merge", handler.mergeROM)
	group.PUT("/:rom_id/discs/order", handler.reorderDiscs)
	group.POST("/:rom_id/discs/:number/split", handler.splitDisc)
	group.DELETE("/:rom_id/discs/:number", handler.removeDisc)
	group.GET("/:rom_id/media/:kind", handler.media)
	group.GET("/:rom_id/download", handler.download)
	group.POST("/:rom_id/download-link", requireService(links != nil), handler.createDownloadLink)
}

// parseROMListRequest reads GET /rom's query: limit, cursor, platform, q (search), identified,
// sort (title, release, publisher, or genre), and order (asc or desc).
func parseROMListRequest(c *gin.Context) (rom.ListRequest, error) {
	request := rom.ListRequest{
		Limit:    defaultROMPageSize,
		Cursor:   optionalQuery(c, "cursor"),
		Platform: optionalQuery(c, "platform"),
		Search:   optionalQuery(c, "q"),
		Sort:     c.Query("sort"),
		Order:    c.Query("order"),
	}
	if raw := c.Query("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return rom.ListRequest{}, fault.Validation("limit must be an integer")
		}
		request.Limit = limit
	}
	if raw := c.Query("identified"); raw != "" {
		identified, err := strconv.ParseBool(raw)
		if err != nil {
			return rom.ListRequest{}, fault.Validation("identified must be a boolean")
		}
		request.Identified = &identified
	}
	return request, nil
}

func (handler romHandler) list(c *gin.Context) {
	request, err := parseROMListRequest(c)
	if err != nil {
		writeError(c, err)
		return
	}
	page, err := handler.roms.List(c.Request.Context(), requesterFromContext(c), request)
	respondJSON(c, http.StatusOK, page, err)
}

// platforms lists the platforms present in the requester's library, for the platform filter.
func (handler romHandler) platforms(c *gin.Context) {
	platforms, err := handler.roms.Platforms(c.Request.Context(), requesterFromContext(c))
	respondJSON(c, http.StatusOK, gin.H{"item": platforms}, err)
}

// platformCatalog lists every known platform, for the platform choice when editing a ROM.
func (handler romHandler) platformCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"item": handler.roms.PlatformCatalog()})
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
	image, err := handler.roms.OpenCover(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"))
	serveImage(c, image, err)
}

func (handler romHandler) coverThumbnail(c *gin.Context) {
	image, err := handler.roms.OpenCoverThumbnail(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"))
	serveImage(c, image, err)
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

// compress asks for a disc kept as tracks to be compressed to CHD in the background.
func (handler romHandler) compress(c *gin.Context) {
	detail, err := handler.roms.RequestCompression(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"))
	respondJSON(c, http.StatusAccepted, detail, err)
}

// mergeRequest names the ROM whose discs move onto the one in the path.
type mergeRequest struct {
	SourceROMID string `json:"source_rom_id"`
}

func (handler romHandler) mergeROM(c *gin.Context) {
	var input mergeRequest
	if !bindJSON(c, &input) {
		return
	}
	merged, err := handler.roms.MergeROM(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"), input.SourceROMID)
	respondJSON(c, http.StatusOK, merged, err)
}

// reorderRequest lists every disc's ROM file ID in the order they are to be numbered.
type reorderRequest struct {
	ROMFileIDs []string `json:"rom_file_ids"`
}

func (handler romHandler) reorderDiscs(c *gin.Context) {
	var input reorderRequest
	if !bindJSON(c, &input) {
		return
	}
	reordered, err := handler.roms.ReorderDiscs(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"), input.ROMFileIDs)
	respondJSON(c, http.StatusOK, reordered, err)
}

func (handler romHandler) splitDisc(c *gin.Context) {
	number, err := discNumber(c)
	if err != nil {
		writeError(c, err)
		return
	}
	split, err := handler.roms.SplitDisc(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"), number)
	respondJSON(c, http.StatusOK, split, err)
}

func (handler romHandler) removeDisc(c *gin.Context) {
	number, err := discNumber(c)
	if err != nil {
		writeError(c, err)
		return
	}
	remaining, err := handler.roms.RemoveDisc(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"), number)
	respondJSON(c, http.StatusOK, remaining, err)
}

func discNumber(c *gin.Context) (int, error) {
	number, err := strconv.Atoi(c.Param("number"))
	if err != nil {
		return 0, fault.Validation("disc number must be an integer")
	}
	return number, nil
}

func (handler romHandler) media(c *gin.Context) {
	image, err := handler.roms.OpenMedia(c.Request.Context(), requesterFromContext(c), c.Param("rom_id"), c.Param("kind"))
	serveImage(c, image, err)
}

func (handler romHandler) download(c *gin.Context) {
	handler.streamDownload(c, requesterFromContext(c), c.Param("rom_id"))
}

// downloadLinkResponse is a URL the browser can open directly to stream a ROM download.
type downloadLinkResponse struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	ExpiresIn int64  `json:"expires_in"`
}

func (handler romHandler) createDownloadLink(c *gin.Context) {
	requester, romID := requesterFromContext(c), c.Param("rom_id")
	// Only sign links for ROMs the requester can access; inaccessible ROMs stay not found.
	format := rom.DownloadFormat(c.Query("format"))
	filename, err := handler.roms.DownloadFilename(c.Request.Context(), requester, romID, format)
	if err != nil {
		writeError(c, err)
		return
	}
	token, expiresAt, err := handler.links.Sign(requester, romID)
	if err != nil {
		writeError(c, fault.Internal(err))
		return
	}
	query := url.Values{"token": {token}}
	if format != "" {
		// The format only picks between downloads the token already allows.
		query.Set("format", string(format))
	}
	c.JSON(http.StatusCreated, downloadLinkResponse{
		URL:       downloadLinkPath + "?" + query.Encode(),
		Filename:  filename,
		ExpiresIn: int64(time.Until(expiresAt).Round(time.Second).Seconds()),
	})
}

func (handler romHandler) downloadWithLink(c *gin.Context) {
	requester, romID, err := handler.links.Verify(c.Query("token"))
	if err != nil {
		writeSpecificError(c, http.StatusForbidden, "invalid_download_link", "download link is invalid or expired")
		return
	}
	// Access is checked again at download time, against the requester the link was issued to.
	handler.streamDownload(c, requester, romID)
}

func (handler romHandler) streamDownload(c *gin.Context, requester identity.Requester, romID string) {
	output, err := handler.roms.DownloadAs(c.Request.Context(), requester, romID, rom.DownloadFormat(c.Query("format")))
	if err != nil {
		writeError(c, err)
		return
	}
	if output.Content.ReadSeekCloser != nil {
		defer output.Content.Close()
	}
	sent := writeDownload(c, output)
	if bodyWasServed(c) && output.Served != nil {
		output.Served(c.Request.Context(), sent)
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

// serveImage streams a stored image with its sniffed type; unrecognized content is treated as
// missing.
//
// Responses carry the image's SHA-256 as their ETag, taken from its content address, so neither
// a full response nor a 304 reads or hashes more than the first bytes. Requests whose URL names
// the image's content version (see rom.ImageVersionParam) are cacheable indefinitely, since a
// changed image gets a new URL; unversioned requests must revalidate, which costs only a 304
// when unchanged.
func serveImage(c *gin.Context, image rom.Image, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	defer image.Close()
	head := make([]byte, 16)
	read, _ := io.ReadFull(image, head)
	contentType := imageContentType(head[:read])
	if _, seekErr := image.Seek(0, io.SeekStart); contentType == "" || seekErr != nil {
		writeError(c, fault.NotFound())
		return
	}
	header := c.Writer.Header()
	header.Set("Content-Type", contentType)
	if image.Version != "" {
		header.Set("ETag", `"`+image.Version+`"`)
	} else {
		header.Set("ETag", fmt.Sprintf(`W/"%x-%x"`, image.Size, image.ModifiedAt.UnixNano()))
	}
	if image.Version != "" && rom.ImageVersionMatches(c.Query(rom.ImageVersionParam), image.Version) {
		header.Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		header.Set("Cache-Control", "private, no-cache")
	}
	serveFile(c, "", image.StoredContent)
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
