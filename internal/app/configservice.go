package app

import (
	"github.com/tomaszcichy9825/culler/internal/config"
)

// ConfigService exposes the settings file to the frontend.
type ConfigService struct {
	app *App
}

// NewConfigService binds the service to the shared state.
func NewConfigService(a *App) *ConfigService {
	return &ConfigService{app: a}
}

// Get returns the configuration the app is running with.
func (s *ConfigService) Get() (config.Config, error) {
	return s.app.Config(), nil
}

// Save validates c, writes it to disk and adopts it. An invalid
// configuration is rejected whole, leaving the running settings untouched.
func (s *ConfigService) Save(c config.Config) error {
	return s.app.setConfig(c)
}

// Path is where the settings file lives, so the UI can point the user at it.
func (s *ConfigService) Path() string {
	return s.app.configPath
}
