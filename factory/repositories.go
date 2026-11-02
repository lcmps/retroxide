package factory

import (
	"context"

	"github.com/lcmps/retroxide/infrastructure/config"
	"github.com/lcmps/retroxide/infrastructure/persistence"
	"gorm.io/gorm"
)

func NewRepositories(ctx context.Context, settings config.Config) (*persistence.Repositories, *gorm.DB, error) {
	database, err := persistence.OpenDatabase(ctx, settings.DBDriver, settings.SQLitePath, settings.MySQLURL)
	if err != nil {
		return nil, nil, err
	}
	return persistence.NewRepositories(database), database, nil
}
