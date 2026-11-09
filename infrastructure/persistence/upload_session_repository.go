package persistence

import (
	"context"
	"time"

	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
)

func (repositories *Repositories) SaveUploadSession(ctx context.Context, session uploadsdomain.Session) error {
	model := uploadSessionModel(session)
	if err := repositories.DB.WithContext(ctx).Create(&model).Error; err != nil {
		return databaseError("saving upload session", err)
	}
	return nil
}

func (repositories *Repositories) FindUploadSession(ctx context.Context, sessionID string) (*uploadsdomain.Session, error) {
	model, err := takeOne[UploadSessionModel](repositories.DB.WithContext(ctx).Where("id = ?", sessionID), "finding upload session")
	return mapOptional(model, err, infallible(uploadSessionEntity))
}

// AdvanceUploadSession records that a session grew from received to session.Received bytes,
// with its new hash progress. It reports false, changing nothing, when the session no longer
// has received bytes: another request for the same part got there first.
func (repositories *Repositories) AdvanceUploadSession(ctx context.Context, session uploadsdomain.Session, received int64) (bool, error) {
	result := repositories.DB.WithContext(ctx).Model(&UploadSessionModel{}).
		Where("id = ? AND received = ?", session.ID, received).
		Updates(map[string]any{"received": session.Received, "hash_state": session.HashState, "updated_at": session.UpdatedAt.Unix()})
	if result.Error != nil {
		return false, databaseError("advancing upload session", result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (repositories *Repositories) DeleteUploadSession(ctx context.Context, sessionID string) error {
	if err := repositories.DB.WithContext(ctx).Where("id = ?", sessionID).Delete(&UploadSessionModel{}).Error; err != nil {
		return databaseError("deleting upload session", err)
	}
	return nil
}

// ListIdleUploadSessions returns the IDs of sessions not updated since before.
func (repositories *Repositories) ListIdleUploadSessions(ctx context.Context, before time.Time) ([]string, error) {
	var ids []string
	if err := repositories.DB.WithContext(ctx).Model(&UploadSessionModel{}).Where("updated_at < ?", before.Unix()).Pluck("id", &ids).Error; err != nil {
		return nil, databaseError("listing idle upload sessions", err)
	}
	return ids, nil
}

func uploadSessionModel(session uploadsdomain.Session) UploadSessionModel {
	return UploadSessionModel{
		ID: session.ID, UploaderID: session.UploaderID, Filename: session.Filename, MIMEType: session.MIMEType,
		Size: session.Size, Received: session.Received, HashState: session.HashState, UpdatedAt: session.UpdatedAt.Unix(),
		Part: session.Part, BIOS: session.BIOS, BIOSPlatform: session.BIOSPlatform,
	}
}

func uploadSessionEntity(model UploadSessionModel) uploadsdomain.Session {
	return uploadsdomain.Session{
		ID: model.ID, UploaderID: model.UploaderID, Filename: model.Filename, MIMEType: model.MIMEType,
		Size: model.Size, Received: model.Received, HashState: model.HashState, UpdatedAt: time.Unix(model.UpdatedAt, 0),
		Part: model.Part, BIOS: model.BIOS, BIOSPlatform: model.BIOSPlatform,
	}
}

func (repositories *Repositories) SaveUploadPart(ctx context.Context, part uploadsdomain.Part) error {
	model := UploadPartModel{
		ID: part.ID, UploaderID: part.UploaderID, Filename: part.Filename, StoragePath: part.StoragePath, Size: part.Size,
		SHA256: part.SHA256, SHA1: part.SHA1, MD5: part.MD5, CRC32: part.CRC32, CreatedAt: part.CreatedAt.Unix(),
	}
	if err := repositories.DB.WithContext(ctx).Create(&model).Error; err != nil {
		return databaseError("saving upload part", err)
	}
	return nil
}

// FindUploadParts returns the parts with ids that belong to uploaderID.
func (repositories *Repositories) FindUploadParts(ctx context.Context, uploaderID string, ids []string) ([]uploadsdomain.Part, error) {
	var models []UploadPartModel
	if err := repositories.DB.WithContext(ctx).Where("uploader_id = ? AND id IN ?", uploaderID, ids).Find(&models).Error; err != nil {
		return nil, databaseError("finding upload parts", err)
	}
	parts := make([]uploadsdomain.Part, len(models))
	for index, model := range models {
		parts[index] = uploadsdomain.Part{
			ID: model.ID, UploaderID: model.UploaderID, Filename: model.Filename, StoragePath: model.StoragePath, Size: model.Size,
			SHA256: model.SHA256, SHA1: model.SHA1, MD5: model.MD5, CRC32: model.CRC32, CreatedAt: time.Unix(model.CreatedAt, 0),
		}
	}
	return parts, nil
}

// DeleteUploadParts forgets parts; their files are left for the storage sweep when unused.
func (repositories *Repositories) DeleteUploadParts(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := repositories.DB.WithContext(ctx).Where("id IN ?", ids).Delete(&UploadPartModel{}).Error; err != nil {
		return databaseError("deleting upload parts", err)
	}
	return nil
}

// DeleteUploadPartsBefore forgets parts created before cutoff and returns how many.
func (repositories *Repositories) DeleteUploadPartsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result := repositories.DB.WithContext(ctx).Where("created_at < ?", cutoff.Unix()).Delete(&UploadPartModel{})
	if result.Error != nil {
		return 0, databaseError("deleting old upload parts", result.Error)
	}
	return result.RowsAffected, nil
}
