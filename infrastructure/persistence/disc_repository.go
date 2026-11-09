package persistence

import (
	"context"
	"errors"

	"github.com/lcmps/retroxide/application/fault"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"gorm.io/gorm"
)

// SaveDisc records a ROM file's format, disc identity, and tracks, replacing any tracks it had.
// A disc identity another file already has is reported as a conflict.
func (repositories *Repositories) SaveDisc(ctx context.Context, romFileID string, format romdomain.FileFormat, identity *string, tracks []romdomain.DiscTrack) error {
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("rom_file_id = ?", romFileID).Delete(&DiscTrackModel{}).Error; err != nil {
			return err
		}
		if len(tracks) > 0 {
			models := make([]DiscTrackModel, len(tracks))
			for index, track := range tracks {
				models[index] = discTrackModel(romFileID, track)
			}
			if err := tx.Create(&models).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&ROMFileModel{}).Where("id = ?", romFileID).
			Updates(map[string]any{"format": string(format), "disc_identity": identity})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fault.NotFound()
		}
		return nil
	})
	if isUniqueViolation(err) {
		return fault.Conflict()
	}
	var appError *fault.Error
	if err != nil && !errors.As(err, &appError) {
		return databaseError("saving disc", err)
	}
	return err
}

// FindDiscTracks returns a ROM file's tracks in order; none for a file that is not a disc.
func (repositories *Repositories) FindDiscTracks(ctx context.Context, romFileID string) ([]romdomain.DiscTrack, error) {
	var models []DiscTrackModel
	if err := repositories.DB.WithContext(ctx).Where("rom_file_id = ?", romFileID).Order("number").Find(&models).Error; err != nil {
		return nil, databaseError("finding disc tracks", err)
	}
	tracks := make([]romdomain.DiscTrack, 0, len(models))
	for _, model := range models {
		track, err := discTrackEntity(model)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}

// FindROMFileByDiscIdentity returns the file holding the disc with identity, if any.
func (repositories *Repositories) FindROMFileByDiscIdentity(ctx context.Context, identity string) (*romdomain.ROMFile, error) {
	model, err := takeOne[ROMFileModel](repositories.DB.WithContext(ctx).Where("disc_identity = ?", identity), "finding rom file by disc identity")
	return mapOptional(model, err, romFileEntity)
}

// SetCompressRequested asks for (or stops asking for) a disc to be compressed to CHD.
func (repositories *Repositories) SetCompressRequested(ctx context.Context, romFileID string, requested bool) error {
	if err := repositories.DB.WithContext(ctx).Model(&ROMFileModel{}).Where("id = ?", romFileID).Update("compress_requested", requested).Error; err != nil {
		return databaseError("requesting compression", err)
	}
	return nil
}

// ListCompressionRequests returns up to limit discs kept as tracks that were asked to be
// compressed, oldest ID first.
func (repositories *Repositories) ListCompressionRequests(ctx context.Context, limit int) ([]romdomain.ROMFile, error) {
	var models []ROMFileModel
	query := repositories.DB.WithContext(ctx).Where("format = ? AND compress_requested = ?", string(romdomain.FormatTracks), true)
	if err := query.Order("id").Limit(limit).Find(&models).Error; err != nil {
		return nil, databaseError("listing compression requests", err)
	}
	files := make([]romdomain.ROMFile, 0, len(models))
	for _, model := range models {
		file, err := romFileEntity(model)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// ListROMFilesByFormat returns up to limit files stored in format, oldest ID first.
func (repositories *Repositories) ListROMFilesByFormat(ctx context.Context, format romdomain.FileFormat, limit int) ([]romdomain.ROMFile, error) {
	var models []ROMFileModel
	if err := repositories.DB.WithContext(ctx).Where("format = ?", string(format)).Order("id").Limit(limit).Find(&models).Error; err != nil {
		return nil, databaseError("listing rom files by format", err)
	}
	files := make([]romdomain.ROMFile, 0, len(models))
	for _, model := range models {
		file, err := romFileEntity(model)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// ReplaceROMFileContent points a ROM file at new stored content (a disc compressed to CHD),
// with its hashes and format, and forgets its tracks' separate files. The ROM file keeps its
// ID, so ROMs and uploads referring to it are unaffected.
func (repositories *Repositories) ReplaceROMFileContent(ctx context.Context, romFileID string, hash romdomain.ROMHash, size uint64, storagePath string, format romdomain.FileFormat) error {
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&ROMFileModel{}).Where("id = ?", romFileID).Updates(map[string]any{
			"sha256": hash.SHA256, "sha1": hash.SHA1, "md5": hash.MD5, "crc32": hash.CRC32,
			"size": size, "storage_path": storagePath, "format": string(format), "compress_requested": false,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fault.NotFound()
		}
		return tx.Model(&DiscTrackModel{}).Where("rom_file_id = ?", romFileID).Update("storage_path", nil).Error
	})
	if isUniqueViolation(err) {
		return fault.Conflict()
	}
	var appError *fault.Error
	if err != nil && !errors.As(err, &appError) {
		return databaseError("replacing rom file content", err)
	}
	return err
}

func discTrackModel(romFileID string, track romdomain.DiscTrack) DiscTrackModel {
	return DiscTrackModel{
		ROMFileID: romFileID, Number: track.Number, Mode: track.Mode, SectorBytes: track.SectorBytes,
		Frames: track.Frames, Pregap: track.Pregap, PregapStored: track.PregapStored, Postgap: track.Postgap,
		Size: track.Size, SHA256: track.Hash.SHA256, SHA1: track.Hash.SHA1, MD5: track.Hash.MD5, CRC32: track.Hash.CRC32,
		Filename: track.Filename, StoragePath: track.StoragePath,
	}
}

func discTrackEntity(model DiscTrackModel) (romdomain.DiscTrack, error) {
	hash, err := romdomain.NewROMHash(model.SHA256, model.SHA1, model.MD5, model.CRC32)
	if err != nil {
		return romdomain.DiscTrack{}, fault.Internal(err)
	}
	return romdomain.DiscTrack{
		Number: model.Number, Mode: model.Mode, SectorBytes: model.SectorBytes, Frames: model.Frames,
		Pregap: model.Pregap, PregapStored: model.PregapStored, Postgap: model.Postgap, Size: model.Size,
		Hash: hash, Filename: model.Filename, StoragePath: model.StoragePath,
	}, nil
}

// DeleteUnusedROMFile forgets a stored file (and its disc tracks) no ROM or BIOS uses; its
// content is left for the storage sweep. A file in use is kept.
func (repositories *Repositories) DeleteUnusedROMFile(ctx context.Context, romFileID string) error {
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		games, bios, err := fileUses(tx, romFileID, "")
		if err != nil || games > 0 || bios > 0 {
			return err
		}
		if err := tx.Where("rom_file_id = ?", romFileID).Delete(&DiscTrackModel{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", romFileID).Delete(&ROMFileModel{}).Error
	})
	if err != nil {
		return databaseError("deleting unused rom file", err)
	}
	return nil
}
