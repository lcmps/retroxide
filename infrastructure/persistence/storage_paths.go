package persistence

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// storagePathColumns are the columns that hold storage keys, as table and column.
var storagePathColumns = [][2]string{
	{"rom_file", "storage_path"},
	{"rom_file", "sheet_path"},
	{"rom", "cover_art_path"},
	{"rom", "cover_thumb_path"},
	{"rom", "title_screen_path"},
	{"rom", "screenshot_path"},
	{"disc_track", "storage_path"},
	{"upload_part", "storage_path"},
}

// notAKey matches values that are not storage keys ("ab/" and a 64-digit address, 67
// characters), such as the absolute paths earlier versions recorded.
const notAKey = " IS NOT NULL AND NOT (LENGTH(%[1]s) = 67 AND %[1]s LIKE '__/%%' AND %[1]s NOT LIKE '/%%')"

// ListLegacyStoragePaths returns every distinct stored path that is not a storage key.
func (repositories *Repositories) ListLegacyStoragePaths(ctx context.Context) ([]string, error) {
	seen := map[string]bool{}
	var paths []string
	for _, column := range storagePathColumns {
		var values []string
		err := repositories.DB.WithContext(ctx).Table(column[0]).
			Where(column[1]+fmt.Sprintf(notAKey, column[1])).Distinct(column[1]).Pluck(column[1], &values).Error
		if err != nil {
			return nil, databaseError("listing legacy storage paths", err)
		}
		for _, value := range values {
			if !seen[value] {
				seen[value] = true
				paths = append(paths, value)
			}
		}
	}
	return paths, nil
}

// ReplaceStoragePaths rewrites every reference to each old path with its key, atomically.
func (repositories *Repositories) ReplaceStoragePaths(ctx context.Context, keys map[string]string) error {
	err := repositories.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for old, key := range keys {
			if old == key {
				continue
			}
			for _, column := range storagePathColumns {
				if err := tx.Table(column[0]).Where(column[1]+" = ?", old).Update(column[1], key).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return databaseError("recording storage keys", err)
	}
	return nil
}

// referenceBatchSize bounds the paths checked per query, well under every dialect's limit on
// query parameters.
const referenceBatchSize = 500

// ReferencedStoragePaths returns which of paths a ROM file or image column still references.
func (repositories *Repositories) ReferencedStoragePaths(ctx context.Context, paths []string) (map[string]bool, error) {
	referenced := make(map[string]bool)
	for start := 0; start < len(paths); start += referenceBatchSize {
		batch := paths[start:min(start+referenceBatchSize, len(paths))]
		for _, column := range storagePathColumns {
			var found []string
			if err := repositories.DB.WithContext(ctx).Table(column[0]).Where(column[1]+" IN ?", batch).
				Distinct(column[1]).Pluck(column[1], &found).Error; err != nil {
				return nil, databaseError("checking storage references", err)
			}
			for _, path := range found {
				referenced[path] = true
			}
		}
	}
	return referenced, nil
}
