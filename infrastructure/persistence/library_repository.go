package persistence

import (
	"context"

	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	"gorm.io/gorm/clause"
)

func (repositories *Repositories) SaveLibrary(ctx context.Context, library librariesdomain.Library) error {
	// A raw insert keeps created_at on its database default.
	if err := repositories.DB.WithContext(ctx).Exec(
		"INSERT INTO library (id, user_id, name) VALUES (?, ?, ?)",
		library.ID, library.UserID, library.Name,
	).Error; err != nil {
		return databaseError("saving library", err)
	}
	return nil
}

func (repositories *Repositories) ListLibrary(ctx context.Context) ([]librariesdomain.Library, error) {
	var models []LibraryModel
	if err := repositories.DB.WithContext(ctx).Order("id ASC").Find(&models).Error; err != nil {
		return nil, databaseError("listing libraries", err)
	}
	libraries := make([]librariesdomain.Library, 0, len(models))
	for _, model := range models {
		libraries = append(libraries, libraryEntity(model))
	}
	return libraries, nil
}

func (repositories *Repositories) FindLibraryByID(ctx context.Context, libraryID string) (*librariesdomain.Library, error) {
	model, err := takeOne[LibraryModel](repositories.DB.WithContext(ctx).Where("id = ?", libraryID), "finding library")
	return mapOptional(model, err, infallible(libraryEntity))
}

func (repositories *Repositories) ListLibraryROMIDs(ctx context.Context, libraryID string) ([]string, error) {
	var romIDs []string
	if err := repositories.DB.WithContext(ctx).Model(&LibraryItemModel{}).
		Where("library_id = ?", libraryID).Order("rom_id ASC").Pluck("rom_id", &romIDs).Error; err != nil {
		return nil, databaseError("listing library ROMs", err)
	}
	return romIDs, nil
}

// AddLibraryItem adds romID to the library; adding an existing item is a no-op.
func (repositories *Repositories) AddLibraryItem(ctx context.Context, libraryID, romID string) error {
	item := LibraryItemModel{LibraryID: libraryID, ROMID: romID}
	if err := repositories.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
		return databaseError("adding library item", err)
	}
	return nil
}

// RemoveLibraryItem reports whether the item was present.
func (repositories *Repositories) RemoveLibraryItem(ctx context.Context, libraryID, romID string) (bool, error) {
	result := repositories.DB.WithContext(ctx).Where("library_id = ? AND rom_id = ?", libraryID, romID).Delete(&LibraryItemModel{})
	if result.Error != nil {
		return false, databaseError("removing library item", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func libraryEntity(model LibraryModel) librariesdomain.Library {
	return librariesdomain.Library{ID: model.ID, UserID: model.UserID, Name: model.Name}
}
