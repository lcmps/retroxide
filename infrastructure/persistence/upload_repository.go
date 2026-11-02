package persistence

import (
	"context"

	"github.com/lcmps/retroxide/application/fault"
	uploadsdomain "github.com/lcmps/retroxide/domain/uploads"
)

// SaveUpload inserts a processing upload, or updates an existing one once it reaches a final state.
func (repositories *Repositories) SaveUpload(ctx context.Context, upload uploadsdomain.Upload) error {
	model := uploadModel(upload)
	if upload.Status == uploadsdomain.UploadStatusProcessing {
		if err := repositories.DB.WithContext(ctx).Save(&model).Error; err != nil {
			return databaseError("saving upload", err)
		}
		return nil
	}
	result := repositories.DB.WithContext(ctx).Model(&UploadModel{}).Where("id = ?", upload.ID).Select("*").Updates(&model)
	if result.Error != nil {
		return databaseError("updating upload", result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

func (repositories *Repositories) FindUpload(ctx context.Context, uploadID string) (*uploadsdomain.Upload, error) {
	model, err := takeOne[UploadModel](repositories.DB.WithContext(ctx).Where("id = ?", uploadID), "finding upload")
	return mapOptional(model, err, infallible(uploadEntity))
}

func uploadModel(upload uploadsdomain.Upload) UploadModel {
	return UploadModel{
		ID: upload.ID, UploaderID: upload.UploaderID, OriginalFilename: upload.OriginalFilename,
		Status: string(upload.Status), ErrorMessage: upload.ErrorMessage, ROMID: upload.ROMID,
	}
}

func uploadEntity(model UploadModel) uploadsdomain.Upload {
	return uploadsdomain.Upload{
		ID: model.ID, UploaderID: model.UploaderID, OriginalFilename: model.OriginalFilename,
		Status: uploadsdomain.UploadStatus(model.Status), ErrorMessage: model.ErrorMessage, ROMID: model.ROMID,
	}
}

func (repositories *Repositories) FindUploadedFilename(ctx context.Context, romID string) (*string, error) {
	var filenames []string
	if err := repositories.DB.WithContext(ctx).Model(&UploadModel{}).
		Where("rom_id = ?", romID).Order("id").Limit(1).
		Pluck("original_filename", &filenames).Error; err != nil {
		return nil, databaseError("finding uploaded filename", err)
	}
	if len(filenames) == 0 {
		return nil, nil
	}
	return &filenames[0], nil
}
