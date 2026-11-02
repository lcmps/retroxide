package factory

import "github.com/lcmps/retroxide/infrastructure/config"

func NewConfig() (config.Config, error) {
	return config.LoadProcessEnvironment()
}
