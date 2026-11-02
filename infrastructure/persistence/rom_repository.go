package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"gorm.io/gorm"
)

// UpsertROMFile inserts romFile, reporting an already-exists conflict when identical content is stored.
func (repositories *Repositories) UpsertROMFile(ctx context.Context, romFile romdomain.ROMFile) (romdomain.ROMFile, error) {
	model := romFileModel(romFile)
	if err := repositories.DB.WithContext(ctx).Create(&model).Error; err != nil {
		existing, lookupErr := repositories.FindROMFileBySHA256(ctx, model.SHA256)
		if lookupErr == nil && existing != nil {
			return romdomain.ROMFile{}, classifyROMFileConflict(existing.ID, existing.Hash, romFile.Hash)
		}
		return romdomain.ROMFile{}, databaseError("upserting rom file", err)
	}
	saved, err := repositories.FindROMFileBySHA256(ctx, model.SHA256)
	if err != nil {
		return romdomain.ROMFile{}, err
	}
	if saved == nil {
		return romdomain.ROMFile{}, fault.Internal(errors.New("rom file was not visible after upsert"))
	}
	return *saved, nil
}

func classifyROMFileConflict(existingID string, existingHash, incomingHash romdomain.ROMHash) error {
	if !existingHash.MatchesContent(incomingHash) {
		return fault.Internal(errors.New("sha256 collision with differing rom hash bundle"))
	}
	return fault.ROMAlreadyExists(existingID, incomingHash.SHA256)
}

func (repositories *Repositories) SaveROM(ctx context.Context, rom romdomain.ROM) error {
	model := romModel(rom)
	if err := repositories.DB.WithContext(ctx).Save(&model).Error; err != nil {
		return databaseError("saving rom", err)
	}
	return nil
}

// ListROMsWithFiles returns up to limit+1 ROMs ordered by ID, so callers can detect a next page.
func (repositories *Repositories) ListROMsWithFiles(ctx context.Context, limit int, filter rom.ROMFilter) ([]romdomain.ROMWithFile, error) {
	query := repositories.DB.WithContext(ctx).
		Table("rom AS r").
		Joins("JOIN rom_file AS f ON f.id = r.rom_file_id")
	if filter.UploaderID != nil {
		query = query.Where("r.uploader_id = ?", *filter.UploaderID)
	}
	if filter.AfterCursor != nil {
		query = query.Where("r.id > ?", *filter.AfterCursor)
	}
	if filter.Platform != nil && *filter.Platform != "" {
		// platform_key is the generated, indexed LOWER(platform) column.
		query = query.Where("r.platform_key = ?", strings.ToLower(*filter.Platform))
	}
	if filter.Search != nil && strings.TrimSpace(*filter.Search) != "" {
		pattern := "%" + escapeLike(strings.ToLower(strings.TrimSpace(*filter.Search))) + "%"
		query = query.Where("(LOWER(r.title) LIKE ? ESCAPE '!' OR r.platform_key LIKE ? ESCAPE '!' OR LOWER(r.id) LIKE ? ESCAPE '!')", pattern, pattern, pattern)
	}
	return scanROMsWithFiles(query.Order("r.id ASC").Limit(limit+1), "listing roms")
}

// likeEscaper escapes LIKE wildcards with '!', which behaves the same in SQLite and MySQL
// (unlike backslash, which MySQL also treats as a string-literal escape).
var likeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

func escapeLike(value string) string {
	return likeEscaper.Replace(value)
}

// scanROMsWithFiles selects ROM summary columns from a query joining rom AS r and rom_file AS f.
func scanROMsWithFiles(query *gorm.DB, operation string) ([]romdomain.ROMWithFile, error) {
	var rows []struct {
		ROMModel
		SHA256 string `gorm:"column:sha256"`
	}
	err := query.
		Select("r.id, r.rom_file_id, r.uploader_id, r.title, r.platform, r.region, r.version, r.identified_by, r.confidence, r.cover_art_path, f.sha256").
		Scan(&rows).Error
	if err != nil {
		return nil, databaseError(operation, err)
	}
	result := make([]romdomain.ROMWithFile, 0, len(rows))
	for _, row := range rows {
		result = append(result, romdomain.ROMWithFile{ROM: romEntity(row.ROMModel), SHA256: row.SHA256})
	}
	return result, nil
}

func (repositories *Repositories) FindROMByID(ctx context.Context, romID string) (*romdomain.ROM, error) {
	model, err := takeOne[ROMModel](repositories.DB.WithContext(ctx).Where("id = ?", romID), "finding rom")
	return mapOptional(model, err, infallible(romEntity))
}

func (repositories *Repositories) FindROMFileByID(ctx context.Context, romFileID string) (*romdomain.ROMFile, error) {
	model, err := takeOne[ROMFileModel](repositories.DB.WithContext(ctx).Where("id = ?", romFileID), "finding rom file")
	return mapOptional(model, err, romFileEntity)
}

func (repositories *Repositories) FindROMFileBySHA256(ctx context.Context, sha256 string) (*romdomain.ROMFile, error) {
	model, err := takeOne[ROMFileModel](repositories.DB.WithContext(ctx).Where("sha256 = ?", sha256), "finding rom file by sha256")
	return mapOptional(model, err, romFileEntity)
}

func (repositories *Repositories) FindROMFileByROMID(ctx context.Context, romID string) (*romdomain.ROMFile, error) {
	query := repositories.DB.WithContext(ctx).Table("rom_file AS f").
		Select("f.*").Joins("JOIN rom AS r ON r.rom_file_id = f.id").
		Where("r.id = ?", romID)
	model, err := takeOne[ROMFileModel](query, "finding rom file by rom id")
	return mapOptional(model, err, romFileEntity)
}

func (repositories *Repositories) CountROMsByROMFileID(ctx context.Context, romFileID string) (uint64, error) {
	var count int64
	if err := repositories.DB.WithContext(ctx).Model(&ROMModel{}).Where("rom_file_id = ?", romFileID).Count(&count).Error; err != nil {
		return 0, databaseError("counting rom references", err)
	}
	return uint64(count), nil
}

func (repositories *Repositories) DeleteROM(ctx context.Context, romID string) error {
	return expectDeleted(repositories.DB.WithContext(ctx).Where("id = ?", romID).Delete(&ROMModel{}), "deleting rom")
}

func (repositories *Repositories) DeleteROMFile(ctx context.Context, romFileID string) error {
	return expectDeleted(repositories.DB.WithContext(ctx).Where("id = ?", romFileID).Delete(&ROMFileModel{}), "deleting rom file")
}

func romModel(rom romdomain.ROM) ROMModel {
	return ROMModel{
		ID: rom.ID, ROMFileID: rom.ROMFileID, UploaderID: rom.UploaderID,
		Title: rom.Identity.Title, Platform: rom.Identity.Platform, Region: rom.Identity.Region,
		Version: rom.Identity.Version, IdentifiedBy: rom.Identity.IdentifiedBy,
		Confidence: rom.Identity.Confidence, CoverArtPath: rom.Identity.CoverArtPath,
		Publisher: rom.Identity.Publisher, ReleaseYear: rom.Identity.ReleaseYear, Description: rom.Identity.Description,
		TitleScreenPath: rom.Identity.TitleScreenPath, ScreenshotPath: rom.Identity.ScreenshotPath,
	}
}

func romEntity(model ROMModel) romdomain.ROM {
	return romdomain.ROM{
		ID: model.ID, ROMFileID: model.ROMFileID, UploaderID: model.UploaderID,
		Identity: romdomain.ROMIdentity{
			Title: model.Title, Platform: model.Platform, Region: model.Region, Version: model.Version,
			CoverArtPath: model.CoverArtPath, IdentifiedBy: model.IdentifiedBy, Confidence: model.Confidence,
			Publisher: model.Publisher, ReleaseYear: model.ReleaseYear, Description: model.Description,
			TitleScreenPath: model.TitleScreenPath, ScreenshotPath: model.ScreenshotPath,
		},
	}
}

func romFileModel(romFile romdomain.ROMFile) ROMFileModel {
	return ROMFileModel{
		ID: romFile.ID, SHA256: romFile.Hash.SHA256, SHA1: romFile.Hash.SHA1,
		MD5: romFile.Hash.MD5, CRC32: romFile.Hash.CRC32, Size: romFile.Size,
		StoragePath: romFile.StoragePath,
	}
}

func romFileEntity(model ROMFileModel) (romdomain.ROMFile, error) {
	hash, err := romdomain.NewROMHash(model.SHA256, model.SHA1, model.MD5, model.CRC32)
	if err != nil {
		return romdomain.ROMFile{}, fault.Internal(fmt.Errorf("invalid persisted rom hash: %w", err))
	}
	return romdomain.ROMFile{ID: model.ID, Hash: hash, Size: model.Size, StoragePath: model.StoragePath}, nil
}
