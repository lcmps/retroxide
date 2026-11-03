package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

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

// ListROMsWithFiles returns up to limit+1 ROMs in filter.Sort order, so callers can detect a
// next page.
func (repositories *Repositories) ListROMsWithFiles(ctx context.Context, limit int, filter rom.ROMFilter) ([]romdomain.ROMWithFile, error) {
	base := func() *gorm.DB {
		query := repositories.DB.WithContext(ctx).
			Table("rom AS r").
			Joins("JOIN rom_file AS f ON f.id = r.rom_file_id")
		if filter.UploaderID != nil {
			query = query.Where("r.uploader_id = ?", *filter.UploaderID)
		}
		if filter.Platform != nil && *filter.Platform != "" {
			// platform_key is the generated, indexed LOWER(platform) column.
			query = query.Where("r.platform_key = ?", strings.ToLower(*filter.Platform))
		}
		if filter.Search != nil && strings.TrimSpace(*filter.Search) != "" {
			query = searchROMs(query, strings.TrimSpace(*filter.Search))
		}
		return query
	}
	column, sorted := sortColumns[filter.Sort.Field]
	if !sorted {
		query := base()
		if filter.After != nil {
			query = query.Where("r.id > ?", filter.After.ID)
		}
		return scanROMsWithFiles(query.Order("r.id ASC").Limit(limit+1), "listing roms")
	}
	return listSortedROMs(base, column, filter, limit+1)
}

// sortColumns maps sort fields to their columns. The listing compares them case-insensitively:
// MySQL through the column collation, SQLite with NOCASE, matching migration 20's indexes.
var sortColumns = map[rom.SortField]string{
	rom.SortTitle:     "r.title",
	rom.SortRelease:   "r.release_year",
	rom.SortPublisher: "r.publisher",
	rom.SortGenre:     "r.genre",
}

// listSortedROMs reads up to count ROMs ordered by column, then ID, in the sort's direction,
// with ROMs lacking a value last. It reads ROMs that have a value first and, if the page is not
// full, continues with those that do not (ordered by ID), so each part walks an index in order
// instead of sorting the whole match.
func listSortedROMs(base func() *gorm.DB, column string, filter rom.ROMFilter, count int) ([]romdomain.ROMWithFile, error) {
	direction, compare := "ASC", ">"
	if filter.Sort.Descending {
		direction, compare = "DESC", "<"
	}
	var rows []romdomain.ROMWithFile
	if filter.After == nil || filter.After.Value != nil {
		ordered := column
		if !isMySQL(base()) {
			ordered += " COLLATE NOCASE"
		}
		query := base().Where(column + " IS NOT NULL")
		if after := filter.After; after != nil {
			query = query.Where(
				fmt.Sprintf("(%[1]s %[2]s ? OR (%[1]s = ? AND r.id %[2]s ?))", ordered, compare),
				*after.Value, *after.Value, after.ID,
			)
		}
		valued, err := scanROMsWithFiles(query.Order(ordered+" "+direction+", r.id "+direction).Limit(count), "listing sorted roms")
		if err != nil || len(valued) >= count {
			return valued, err
		}
		rows = valued
	}
	query := base().Where(column + " IS NULL")
	if filter.After != nil && filter.After.Value == nil {
		query = query.Where("r.id > ?", filter.After.ID)
	}
	missing, err := scanROMsWithFiles(query.Order("r.id ASC").Limit(count-len(rows)), "listing sorted roms")
	if err != nil {
		return nil, err
	}
	return append(rows, missing...), nil
}

const (
	// sqliteTrigramLength is the shortest term the trigram index can match.
	sqliteTrigramLength = 3
	// mysqlMinTokenLength is InnoDB's default innodb_ft_min_token_size, the shortest indexed word.
	mysqlMinTokenLength = 3
)

// searchROMs matches term case-insensitively against the title or platform, using a full-text
// index so a search does not read every ROM; terms too short for the index fall back to a scan.
// SQLite's trigram index matches any substring. MySQL's word index matches words by prefix, then
// a LIKE confirms the whole term appears as typed, so "mario k" finds "Super Mario Kart" but a
// fragment from inside a word ("ario") does not. Both dialects' LIKE is already case-insensitive
// (ASCII in SQLite, the column collation in MySQL), so no LOWER() is needed.
func searchROMs(query *gorm.DB, term string) *gorm.DB {
	pattern := "%" + escapeLike(term) + "%"
	substring := "(r.title LIKE ? ESCAPE '!' OR r.platform LIKE ? ESCAPE '!')"
	if isMySQL(query) {
		if words := mysqlFullTextWords(term); words != "" {
			return query.Where("MATCH(r.title, r.platform) AGAINST (? IN BOOLEAN MODE) AND "+substring, words, pattern, pattern)
		}
		return query.Where(substring, pattern, pattern)
	}
	if utf8.RuneCountInString(term) < sqliteTrigramLength {
		return query.Where(substring, pattern, pattern)
	}
	// A quoted FTS5 phrase is matched as a substring by the trigram tokenizer.
	phrase := `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	return query.Where("r.id IN (SELECT k.rom_id FROM rom_search AS s JOIN rom_search_key AS k ON k.id = s.rowid WHERE rom_search MATCH ?)", phrase)
}

// mysqlFullTextWords builds a boolean-mode query requiring every word of term long enough to be
// indexed, as a prefix ("+mario* +kart*"), or "" when no word is. Words are split the way InnoDB
// tokenizes, on anything but letters and digits, which also drops boolean operators and quotes.
func mysqlFullTextWords(term string) string {
	var required []string
	for _, word := range strings.FieldsFunc(term, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if utf8.RuneCountInString(word) >= mysqlMinTokenLength {
			required = append(required, "+"+word+"*")
		}
	}
	return strings.Join(required, " ")
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
		Select("r.id, r.rom_file_id, r.uploader_id, r.title, r.platform, r.region, r.version, r.identified_by, r.confidence, r.cover_art_path, r.publisher, r.release_year, r.genre, f.sha256").
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
		Publisher: rom.Identity.Publisher, ReleaseYear: rom.Identity.ReleaseYear, Genre: rom.Identity.Genre, Description: rom.Identity.Description,
		TitleScreenPath: rom.Identity.TitleScreenPath, ScreenshotPath: rom.Identity.ScreenshotPath,
	}
}

func romEntity(model ROMModel) romdomain.ROM {
	return romdomain.ROM{
		ID: model.ID, ROMFileID: model.ROMFileID, UploaderID: model.UploaderID,
		Identity: romdomain.ROMIdentity{
			Title: model.Title, Platform: model.Platform, Region: model.Region, Version: model.Version,
			CoverArtPath: model.CoverArtPath, IdentifiedBy: model.IdentifiedBy, Confidence: model.Confidence,
			Publisher: model.Publisher, ReleaseYear: model.ReleaseYear, Genre: model.Genre, Description: model.Description,
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

// ListPlatforms groups by the indexed, case-folded platform_key so values differing only in case
// count as one platform.
func (repositories *Repositories) ListPlatforms(ctx context.Context, uploaderID *string) ([]rom.PlatformCount, error) {
	var rows []struct {
		Platform string
		Count    uint64
	}
	query := repositories.DB.WithContext(ctx).Model(&ROMModel{}).
		Select("MIN(platform) AS platform, COUNT(*) AS count").Group("platform_key")
	if uploaderID != nil {
		query = query.Where("uploader_id = ?", *uploaderID)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, databaseError("listing platforms", err)
	}
	counts := make([]rom.PlatformCount, 0, len(rows))
	for _, row := range rows {
		counts = append(counts, rom.PlatformCount{Platform: row.Platform, Count: row.Count})
	}
	return counts, nil
}
