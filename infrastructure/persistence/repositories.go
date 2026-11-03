package persistence

import (
	"errors"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/libraries"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
	"gorm.io/gorm"
)

const sqlTimestampLayout = "2006-01-02 15:04:05"

// Repositories implements every application repository on a single GORM connection.
// Methods are grouped by aggregate in the *_repository.go files of this package.
type Repositories struct {
	DB *gorm.DB
}

func NewRepositories(db *gorm.DB) *Repositories {
	return &Repositories{DB: db}
}

var (
	_ rom.Repository              = (*Repositories)(nil)
	_ uploads.Repository          = (*Repositories)(nil)
	_ libraries.Repository        = (*Repositories)(nil)
	_ auth.UserRepository         = (*Repositories)(nil)
	_ auth.ClientRepository       = (*Repositories)(nil)
	_ auth.RefreshTokenRepository = (*Repositories)(nil)
)

// takeOne loads the single row matched by query, returning nil without error when none exists.
func takeOne[M any](query *gorm.DB, operation string) (*M, error) {
	var model M
	if err := query.Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, databaseError(operation, err)
	}
	return &model, nil
}

// mapOptional converts an optional model into an optional entity.
func mapOptional[M, E any](model *M, err error, convert func(M) (E, error)) (*E, error) {
	if err != nil || model == nil {
		return nil, err
	}
	entity, err := convert(*model)
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// infallible adapts a conversion that cannot fail for use with mapOptional.
func infallible[M, E any](convert func(M) E) func(M) (E, error) {
	return func(model M) (E, error) { return convert(model), nil }
}

// expectDeleted reports not-found when a delete statement matched no row.
func expectDeleted(result *gorm.DB, operation string) error {
	if result.Error != nil {
		return databaseError(operation, result.Error)
	}
	if result.RowsAffected == 0 {
		return fault.NotFound()
	}
	return nil
}

func isMySQL(db *gorm.DB) bool {
	return db.Dialector.Name() == "mysql"
}

func databaseError(operation string, err error) error {
	return fault.Internal(fmt.Errorf("%s: %w", operation, err))
}

func formatUnixTimestamp(seconds uint64) (string, error) {
	if seconds > uint64(1<<63-1) {
		return "", fault.Validation("timestamp is outside supported range")
	}
	return time.Unix(int64(seconds), 0).UTC().Format(sqlTimestampLayout), nil
}

func parseUnixTimestamp(timestamp string) (uint64, error) {
	parsed, err := time.ParseInLocation(sqlTimestampLayout, timestamp, time.UTC)
	if err != nil {
		return 0, err
	}
	seconds := parsed.Unix()
	if seconds < 0 {
		return 0, fmt.Errorf("timestamp predates unix epoch")
	}
	return uint64(seconds), nil
}

// isUniqueViolation reports whether err is a unique-constraint violation on either dialect.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062 // ER_DUP_ENTRY
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
