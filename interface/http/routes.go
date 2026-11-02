package http

import (
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	authfeature "github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	libraryfeature "github.com/lcmps/retroxide/application/libraries"
	romfeature "github.com/lcmps/retroxide/application/rom"
	uploadfeature "github.com/lcmps/retroxide/application/uploads"
)

const maxCoverArtBytes = 5 << 20

func registerRoutes(router *gin.Engine, dependencies Dependencies, options Options) {
	api := router.Group("/api/v1")
	api.POST("/session", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Sessions) {
			return
		}
		var input authfeature.LoginRequest
		if !bindJSON(context, &input) {
			return
		}
		if err := authfeature.ValidateEmail(input.Email); err != nil {
			writeError(context, err)
			return
		}
		response, err := dependencies.Sessions.Login(context.Request.Context(), input)
		if err != nil {
			if fault.IsCode(err, fault.CodeUnauthorized) {
				writeSpecificError(context, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
				return
			}
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, response)
	})
	api.POST("/session/refresh", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Sessions) {
			return
		}
		var input authfeature.RefreshRequest
		if !bindJSON(context, &input) {
			return
		}
		response, err := dependencies.Sessions.Refresh(context.Request.Context(), input.RefreshToken)
		if err != nil {
			if fault.IsCode(err, fault.CodeUnauthorized) {
				writeSpecificError(context, http.StatusUnauthorized, "invalid_refresh_token", "refresh token is invalid or expired")
				return
			}
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, response)
	})
	api.DELETE("/session", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Sessions) {
			return
		}
		if err := dependencies.Sessions.Logout(context.Request.Context(), requesterFromContext(context)); err != nil {
			writeError(context, err)
			return
		}
		context.Status(http.StatusNoContent)
	})

	api.GET("/i18n/:locale", func(context *gin.Context) {
		catalog, ok := dependencies.Locales[context.Param("locale")]
		if !ok {
			writeError(context, fault.NotFound())
			return
		}
		context.Header("Content-Type", "application/json")
		context.Header("Cache-Control", "public, max-age=3600")
		context.Data(http.StatusOK, "application/json", catalog)
	})

	api.POST("/upload", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Uploads) {
			return
		}
		if options.MaxUploadBytes > 0 {
			context.Request.Body = http.MaxBytesReader(context.Writer, context.Request.Body, requestBodyLimit(options.MaxUploadBytes))
		}
		fileHeader, err := context.FormFile("file")
		if err != nil {
			if isBodyTooLarge(err) {
				writeError(context, &fault.Error{Code: fault.CodeUploadTooLarge, Message: "upload size exceeds limit while reading multipart body"})
				return
			}
			writeError(context, fault.Validation("missing or empty \"file\" field"))
			return
		}
		if options.MaxUploadBytes > 0 && fileHeader.Size > options.MaxUploadBytes {
			writeError(context, fault.UploadTooLarge(fileHeader.Size, options.MaxUploadBytes))
			return
		}
		file, err := fileHeader.Open()
		if err != nil {
			writeError(context, fault.Validation("invalid multipart file"))
			return
		}
		defer file.Close()
		limit := options.MaxUploadBytes
		if limit <= 0 {
			limit = 100 << 20
		}
		content, err := io.ReadAll(io.LimitReader(file, limit+1))
		if err != nil {
			writeError(context, fault.Validation("invalid multipart file payload"))
			return
		}
		if int64(len(content)) > limit {
			writeError(context, fault.UploadTooLarge(int64(len(content)), limit))
			return
		}
		mimeType := fileHeader.Header.Get("Content-Type")
		var mimeTypePointer *string
		if mimeType != "" {
			mimeTypePointer = &mimeType
		}
		filename := fileHeader.Filename
		if filename == "" {
			filename = "upload.rom"
		}
		input := uploadfeature.UploadInput{UploaderID: requesterFromContext(context).UserID, Filename: filename, MIMEType: mimeTypePointer, Bytes: content}
		accepted, err := dependencies.Uploads.Create(context.Request.Context(), requesterFromContext(context), input)
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusAccepted, accepted)
	})
	api.GET("/upload/:upload_id", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Uploads) {
			return
		}
		status, err := dependencies.Uploads.Status(context.Request.Context(), requesterFromContext(context), context.Param("upload_id"))
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, status)
	})

	api.GET("/rom", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.ROMs) {
			return
		}
		limit := 50
		if rawLimit := context.Query("limit"); rawLimit != "" {
			parsed, err := strconv.Atoi(rawLimit)
			if err != nil || parsed < 0 {
				writeError(context, fault.Validation("limit must be an integer"))
				return
			}
			limit = parsed
		}
		cursor := optionalQuery(context, "cursor")
		platform := optionalQuery(context, "platform")
		var identified *bool
		if raw := context.Query("identified"); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				writeError(context, fault.Validation("identified must be a boolean"))
				return
			}
			identified = &parsed
		}
		page, err := dependencies.ROMs.List(context.Request.Context(), limit, cursor, platform, identified)
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, page)
	})
	api.GET("/rom/:rom_id", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.ROMs) {
			return
		}
		rom, err := dependencies.ROMs.Get(context.Request.Context(), requesterFromContext(context), context.Param("rom_id"))
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, rom)
	})
	api.PATCH("/rom/:rom_id", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.ROMs) {
			return
		}
		var input romfeature.UpdateROMRequest
		if !bindJSON(context, &input) {
			return
		}
		rom, err := dependencies.ROMs.Update(context.Request.Context(), requesterFromContext(context), context.Param("rom_id"), input)
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, rom)
	})
	api.DELETE("/rom/:rom_id", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.ROMs) {
			return
		}
		if err := dependencies.ROMs.Delete(context.Request.Context(), requesterFromContext(context), context.Param("rom_id")); err != nil {
			writeError(context, err)
			return
		}
		context.Status(http.StatusNoContent)
	})
	api.POST("/rom/:rom_id/cover", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.ROMs) {
			return
		}
		context.Request.Body = http.MaxBytesReader(context.Writer, context.Request.Body, maxCoverArtBytes+(1<<20))
		multipartReader, err := context.Request.MultipartReader()
		if err != nil {
			writeError(context, fault.Validation("invalid cover upload"))
			return
		}
		part, err := multipartReader.NextPart()
		if err != nil {
			writeError(context, fault.Validation("cover file is required"))
			return
		}
		defer part.Close()
		content, err := io.ReadAll(io.LimitReader(part, maxCoverArtBytes+1))
		if err != nil || len(content) == 0 || len(content) > maxCoverArtBytes || imageContentType(content) == "" {
			writeError(context, fault.Validation("cover must be a PNG, JPEG, or WebP image up to 5 MiB"))
			return
		}
		rom, err := dependencies.ROMs.UploadCover(context.Request.Context(), requesterFromContext(context), context.Param("rom_id"), content)
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, rom)
	})
	api.GET("/rom/:rom_id/cover", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.ROMs) {
			return
		}
		content, err := dependencies.ROMs.ReadCover(context.Request.Context(), requesterFromContext(context), context.Param("rom_id"))
		if err != nil {
			writeError(context, err)
			return
		}
		contentType := imageContentType(content)
		if contentType == "" {
			writeError(context, fault.NotFound())
			return
		}
		context.Data(http.StatusOK, contentType, content)
	})
	api.POST("/rom/:rom_id/scrape", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.ROMs) {
			return
		}
		rom, err := dependencies.ROMs.Scrape(context.Request.Context(), requesterFromContext(context), context.Param("rom_id"))
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, rom)
	})
	api.GET("/rom/:rom_id/media/:kind", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.ROMs) {
			return
		}
		content, err := dependencies.ROMs.ReadMedia(context.Request.Context(), requesterFromContext(context), context.Param("rom_id"), context.Param("kind"))
		if err != nil {
			writeError(context, err)
			return
		}
		contentType := imageContentType(content)
		if contentType == "" {
			writeError(context, fault.NotFound())
			return
		}
		context.Data(http.StatusOK, contentType, content)
	})
	api.GET("/rom/:rom_id/download", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.ROMs) {
			return
		}
		output, err := dependencies.ROMs.Download(context.Request.Context(), requesterFromContext(context), context.Param("rom_id"))
		if err != nil {
			writeError(context, err)
			return
		}
		writeDownload(context, output)
	})

	api.GET("/library", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Libraries) {
			return
		}
		libraries, err := dependencies.Libraries.List(context.Request.Context(), requesterFromContext(context))
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, libraries)
	})
	api.POST("/library", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Libraries) {
			return
		}
		var input libraryfeature.CreateLibraryRequest
		if !bindJSON(context, &input) {
			return
		}
		library, err := dependencies.Libraries.Create(context.Request.Context(), requesterFromContext(context), input.Name)
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusCreated, library)
	})
	api.GET("/library/:library_id", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Libraries) {
			return
		}
		library, err := dependencies.Libraries.Get(context.Request.Context(), requesterFromContext(context), context.Param("library_id"))
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, library)
	})
	api.POST("/library/:library_id/item", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Libraries) {
			return
		}
		var input libraryfeature.AddLibraryItemRequest
		if !bindJSON(context, &input) {
			return
		}
		if err := dependencies.Libraries.AddItem(context.Request.Context(), requesterFromContext(context), context.Param("library_id"), input.ROMID); err != nil {
			writeError(context, err)
			return
		}
		context.Status(http.StatusNoContent)
	})
	api.DELETE("/library/:library_id/item/:rom_id", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Libraries) {
			return
		}
		if err := dependencies.Libraries.RemoveItem(context.Request.Context(), requesterFromContext(context), context.Param("library_id"), context.Param("rom_id")); err != nil {
			writeError(context, err)
			return
		}
		context.Status(http.StatusNoContent)
	})

	api.GET("/client", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Clients) {
			return
		}
		clients, err := dependencies.Clients.List(context.Request.Context(), requesterFromContext(context))
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusOK, clients)
	})
	api.POST("/client", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Clients) {
			return
		}
		var input authfeature.CreateClientRequest
		if !bindJSON(context, &input) {
			return
		}
		client, err := dependencies.Clients.Create(context.Request.Context(), requesterFromContext(context), input.Name)
		if err != nil {
			writeError(context, err)
			return
		}
		context.JSON(http.StatusCreated, client)
	})
	api.DELETE("/client/:id", func(context *gin.Context) {
		if !requireDependencies(context, dependencies.Clients) {
			return
		}
		if err := dependencies.Clients.Revoke(context.Request.Context(), requesterFromContext(context), context.Param("id")); err != nil {
			writeError(context, err)
			return
		}
		context.Status(http.StatusNoContent)
	})
}

func requestBodyLimit(fileLimit int64) int64 {
	const multipartOverhead = int64(1 << 20)
	if fileLimit > math.MaxInt64-multipartOverhead {
		return math.MaxInt64
	}
	return fileLimit + multipartOverhead
}

func optionalQuery(context *gin.Context, key string) *string {
	value, present := context.GetQuery(key)
	if !present {
		return nil
	}
	return &value
}

func writeDownload(context *gin.Context, output romfeature.DownloadOutput) {
	etag := `"` + output.SHA256 + `"`
	cacheControl := "private, max-age=0, must-revalidate"
	lastModified := ""
	if output.LastModifiedUnixSeconds != nil {
		lastModified = time.Unix(int64(*output.LastModifiedUnixSeconds), 0).UTC().Format(http.TimeFormat)
	}
	context.Header("ETag", etag)
	context.Header("Accept-Ranges", "bytes")
	context.Header("Cache-Control", cacheControl)
	if lastModified != "" {
		context.Header("Last-Modified", lastModified)
	}
	if context.GetHeader("If-None-Match") == etag {
		context.Status(http.StatusNotModified)
		context.Writer.WriteHeaderNow()
		return
	}
	if lastModified != "" {
		if since, err := http.ParseTime(context.GetHeader("If-Modified-Since")); err == nil && !time.Unix(int64(*output.LastModifiedUnixSeconds), 0).After(since) {
			context.Status(http.StatusNotModified)
			context.Writer.WriteHeaderNow()
			return
		}
	}
	filename := filepath.Base(strings.ReplaceAll(output.Filename, "\\", "_"))
	contentDisposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if contentDisposition == "" {
		contentDisposition = `attachment; filename="download.rom"`
	}
	context.Header("Content-Disposition", contentDisposition)
	context.Header("Content-Type", output.MIMEType)
	content := output.Bytes
	if rangeHeader := context.GetHeader("Range"); rangeHeader != "" {
		start, end, ok := parseByteRange(rangeHeader, len(content))
		if !ok {
			context.Header("Content-Range", fmt.Sprintf("bytes */%d", len(content)))
			context.Status(http.StatusRequestedRangeNotSatisfiable)
			context.Writer.WriteHeaderNow()
			return
		}
		content = content[start : end+1]
		context.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(output.Bytes)))
		context.Header("Content-Length", strconv.Itoa(len(content)))
		context.Data(http.StatusPartialContent, output.MIMEType, content)
		return
	}
	context.Header("Content-Length", strconv.Itoa(len(content)))
	context.Data(http.StatusOK, output.MIMEType, content)
}

func parseByteRange(value string, total int) (int, int, bool) {
	if total <= 0 || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, false
	}
	startPart, endPart, found := strings.Cut(strings.TrimPrefix(value, "bytes="), "-")
	if !found {
		return 0, 0, false
	}
	if startPart == "" {
		suffix, err := strconv.Atoi(endPart)
		if err != nil || suffix <= 0 {
			return 0, 0, false
		}
		start := max(total-suffix, 0)
		return start, total - 1, true
	}
	start, err := strconv.Atoi(startPart)
	if err != nil || start < 0 || start >= total {
		return 0, 0, false
	}
	end := total - 1
	if endPart != "" {
		parsedEnd, err := strconv.Atoi(endPart)
		if err != nil || parsedEnd < start {
			return 0, 0, false
		}
		end = min(parsedEnd, total-1)
	}
	return start, end, true
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

func isBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}
