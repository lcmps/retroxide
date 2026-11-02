package persistence

import (
	"context"

	librariesdomain "github.com/lcmps/retroxide/domain/libraries"
	romdomain "github.com/lcmps/retroxide/domain/rom"
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

func (repositories *Repositories) ListLibrary(ctx context.Context, ownerID *string) ([]librariesdomain.Library, error) {
	query := repositories.DB.WithContext(ctx)
	if ownerID != nil {
		query = query.Where("user_id = ?", *ownerID)
	}
	var models []LibraryModel
	if err := query.Order("id ASC").Find(&models).Error; err != nil {
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

func (repositories *Repositories) ListLibraryROMs(ctx context.Context, libraryID string) ([]romdomain.ROMWithFile, error) {
	query := repositories.DB.WithContext(ctx).
		Table("library_item AS i").
		Joins("JOIN rom AS r ON r.id = i.rom_id").
		Joins("JOIN rom_file AS f ON f.id = r.rom_file_id").
		Where("i.library_id = ?", libraryID).
		Order("r.id ASC")
	return scanROMsWithFiles(query, "listing library ROMs")
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
