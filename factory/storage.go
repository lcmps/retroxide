package factory

import (
	"github.com/lcmps/retroxide/infrastructure/config"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

func NewStorage(settings config.Config) *storage.Filesystem {
	return storage.New(settings.StorageRoot)
}
