package observability

import (
	"context"
	"log/slog"

	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
)

type AuditLogger struct {
	logger *slog.Logger
}

func NewAuditLogger(logger *slog.Logger) *AuditLogger {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuditLogger{logger: logger}
}

func (audit *AuditLogger) UploadAccepted(ctx context.Context, uploadID, romFileID, sha256 string, sizeBytes uint64) {
	audit.logger.InfoContext(ctx, "audit event",
		"event", "audit",
		"action", "upload_accepted",
		"upload_id", uploadID,
		"rom_file_id", romFileID,
		"sha256_prefix", sha256Prefix(sha256),
		"size_bytes", sizeBytes,
	)
}

func (audit *AuditLogger) DuplicateUpload(ctx context.Context, romFileID, sha256 string) {
	audit.logger.InfoContext(ctx, "audit event",
		"event", "audit",
		"action", "upload_rejected_duplicate_hash",
		"rom_file_id", romFileID,
		"sha256_prefix", sha256Prefix(sha256),
	)
}

func (audit *AuditLogger) DownloadServed(ctx context.Context, romID, romFileID, sha256 string, sizeBytes uint64) {
	audit.logger.InfoContext(ctx, "audit event",
		"event", "audit",
		"action", "download_served",
		"rom_id", romID,
		"rom_file_id", romFileID,
		"sha256_prefix", sha256Prefix(sha256),
		"size_bytes", sizeBytes,
	)
}

func sha256Prefix(value string) string {
	const prefixLength = 12
	if len(value) <= prefixLength {
		return value
	}
	return value[:prefixLength]
}

var _ uploads.UploadAudit = (*AuditLogger)(nil)
var _ rom.Audit = (*AuditLogger)(nil)
