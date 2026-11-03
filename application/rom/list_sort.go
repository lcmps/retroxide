package rom

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// parseSort reads a listing's sort and order parameters; an empty sort keeps the default order.
func parseSort(field, order string) (ROMSort, error) {
	sort := ROMSort{Field: SortField(strings.ToLower(strings.TrimSpace(field)))}
	switch sort.Field {
	case SortDefault, SortTitle, SortRelease, SortPublisher, SortGenre:
	default:
		return ROMSort{}, fault.Validation("sort must be one of title, release, publisher, or genre")
	}
	switch strings.ToLower(strings.TrimSpace(order)) {
	case "", "asc":
	case "desc":
		sort.Descending = true
	default:
		return ROMSort{}, fault.Validation("order must be asc or desc")
	}
	if sort.Field == SortDefault && sort.Descending {
		return ROMSort{}, fault.Validation("order requires a sort")
	}
	return sort, nil
}

// SortValue returns rom's value for field, or nil when it has none.
func SortValue(rom romdomain.ROM, field SortField) *string {
	switch field {
	case SortTitle:
		return &rom.Identity.Title
	case SortRelease:
		return rom.Identity.ReleaseYear
	case SortPublisher:
		return rom.Identity.Publisher
	case SortGenre:
		return rom.Identity.Genre
	default:
		return nil
	}
}

// sortedCursor is the opaque cursor of a sorted listing. Sort records the order it belongs to,
// so a cursor is not reused with another order, which would skip or repeat ROMs.
type sortedCursor struct {
	Sort  string  `json:"s"`
	Value *string `json:"v"`
	ID    string  `json:"id"`
}

func (sort ROMSort) key() string {
	if sort.Descending {
		return string(sort.Field) + ":desc"
	}
	return string(sort.Field) + ":asc"
}

// encodeCursor returns the cursor of the page after rom. The default order keeps the plain ROM
// ID it has always used, so existing clients keep working.
func encodeCursor(rom romdomain.ROM, sort ROMSort) string {
	if sort.Field == SortDefault {
		return rom.ID
	}
	encoded, _ := json.Marshal(sortedCursor{Sort: sort.key(), Value: SortValue(rom, sort.Field), ID: rom.ID})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeCursor(raw string, sort ROMSort) (*ROMCursor, error) {
	if sort.Field == SortDefault {
		return &ROMCursor{ID: raw}, nil
	}
	invalid := fault.Validation("cursor does not belong to this sort order")
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalid
	}
	var cursor sortedCursor
	if json.Unmarshal(decoded, &cursor) != nil || cursor.Sort != sort.key() || cursor.ID == "" {
		return nil, invalid
	}
	return &ROMCursor{ID: cursor.ID, Value: cursor.Value}, nil
}
