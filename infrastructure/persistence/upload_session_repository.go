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
	}
}

func uploadSessionEntity(model UploadSessionModel) uploadsdomain.Session {
	return uploadsdomain.Session{
		ID: model.ID, UploaderID: model.UploaderID, Filename: model.Filename, MIMEType: model.MIMEType,
		Size: model.Size, Received: model.Received, HashState: model.HashState, UpdatedAt: time.Unix(model.UpdatedAt, 0),
	}
}
