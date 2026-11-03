// Package settings exposes the application's configuration: everyone signed in can read the
// settings that affect them, and administrators can change the ones safe to edit at runtime.
// Changes are persisted to the configuration file and take effect on the next start.
package settings

import (
	"context"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/appearance"
	"github.com/lcmps/retroxide/domain/identity"
)

// Source says where a setting's configured value comes from.
type Source string

const (
	SourceDefault     Source = "default"
	SourceConfigFile  Source = "config_file"
	SourceEnvironment Source = "environment"
)

// UIFontKey selects the interface font: one of the UIFont* values.
const UIFontKey = "RETROXIDE_UI_FONT"

// The interface fonts; see the appearance domain package.
const (
	UIFontPixel  = appearance.FontPixel
	UIFontUbuntu = appearance.FontUbuntu
	UIFontSystem = appearance.FontSystem
)

// Option is one allowed value of a setting with a fixed set of choices.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Setting is one configuration variable as the store sees it.
type Setting struct {
	Key         string
	Label       string
	Description string
	Group       string
	// Value is the value the running application uses.
	Value string
	// Configured is the value the next start will use; it differs from Value after an edit.
	Configured string
	Source     Source
	// Editable settings may be changed from the web interface, unless the process environment
	// sets them (the environment overrides the configuration file).
	Editable bool
	// Public settings are visible to every signed-in user; the rest only to administrators.
	Public bool
	// Live settings take effect as soon as they are saved instead of after a restart.
	Live bool
	// Options lists the allowed values of a setting with a fixed set of choices.
	Options []Option
}

// Store reads and persists configuration.
type Store interface {
	Settings() ([]Setting, error)
	// Update validates the configuration that would result from values and persists it. An
	// empty value removes the setting from the configuration file, restoring its default.
	Update(values map[string]string) error
}

type SettingDTO struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Group       string `json:"group"`
	Value       string `json:"value"`
	// PendingValue is set when a saved change is waiting for a restart.
	PendingValue *string  `json:"pending_value"`
	Source       Source   `json:"source"`
	Editable     bool     `json:"editable"`
	Live         bool     `json:"live"`
	Options      []Option `json:"options,omitempty"`
}

// AppearanceResponse is the presentation configuration every visitor needs, signed in or not.
type AppearanceResponse struct {
	Font string `json:"font"`
}

type ListResponse struct {
	Item []SettingDTO `json:"item"`
	// CanEdit is whether the requester may change editable settings.
	CanEdit bool `json:"can_edit"`
	// RestartRequired is whether saved changes are waiting for a restart.
	RestartRequired bool `json:"restart_required"`
}

type UpdateRequest struct {
	Values map[string]string `json:"values"`
}

type API interface {
	List(context.Context, identity.Requester) (ListResponse, error)
	Update(context.Context, identity.Requester, UpdateRequest) (ListResponse, error)
	Appearance(context.Context) (AppearanceResponse, error)
}

type Service struct {
	store Store
}

var _ API = (*Service)(nil)

func NewService(store Store) *Service {
	return &Service{store: store}
}

// List returns the settings visible to the requester. Only administrators see infrastructure
// settings, and only they see settings as editable.
func (service *Service) List(_ context.Context, requester identity.Requester) (ListResponse, error) {
	if !isUser(requester) {
		return ListResponse{}, fault.Forbidden()
	}
	return service.list(requester)
}

// Update applies an administrator's changes to editable settings.
func (service *Service) Update(_ context.Context, requester identity.Requester, request UpdateRequest) (ListResponse, error) {
	if !requester.IsAdmin() {
		return ListResponse{}, fault.Forbidden()
	}
	if len(request.Values) == 0 {
		return ListResponse{}, fault.Validation("no settings to update")
	}
	current, err := service.store.Settings()
	if err != nil {
		return ListResponse{}, err
	}
	byKey := make(map[string]Setting, len(current))
	for _, setting := range current {
		byKey[setting.Key] = setting
	}
	for key := range request.Values {
		setting, ok := byKey[key]
		switch {
		case !ok:
			return ListResponse{}, fault.Validation("unknown setting " + key)
		case !setting.Editable:
			return ListResponse{}, fault.Validation(key + " cannot be changed from the web interface")
		case setting.Source == SourceEnvironment:
			return ListResponse{}, fault.Validation(key + " is set by the process environment, which overrides the configuration file")
		}
	}
	if err := service.store.Update(request.Values); err != nil {
		return ListResponse{}, err
	}
	return service.list(requester)
}

// Appearance returns the configured interface font; it is public so the sign-in page uses it too.
func (service *Service) Appearance(context.Context) (AppearanceResponse, error) {
	current, err := service.store.Settings()
	if err != nil {
		return AppearanceResponse{}, err
	}
	for _, setting := range current {
		if setting.Key == UIFontKey && setting.Configured != "" {
			return AppearanceResponse{Font: setting.Configured}, nil
		}
	}
	return AppearanceResponse{Font: UIFontPixel}, nil
}

func (service *Service) list(requester identity.Requester) (ListResponse, error) {
	current, err := service.store.Settings()
	if err != nil {
		return ListResponse{}, err
	}
	admin := requester.IsAdmin()
	response := ListResponse{Item: make([]SettingDTO, 0, len(current)), CanEdit: admin}
	for _, setting := range current {
		if !setting.Public && !admin {
			continue
		}
		dto := SettingDTO{
			Key: setting.Key, Label: setting.Label, Description: setting.Description, Group: setting.Group,
			Value: setting.Value, Source: setting.Source, Live: setting.Live, Options: setting.Options,
			Editable: admin && setting.Editable && setting.Source != SourceEnvironment,
		}
		// A live setting already runs with its saved value; nothing waits for a restart.
		if setting.Live {
			dto.Value = setting.Configured
		} else if setting.Configured != setting.Value {
			pending := setting.Configured
			dto.PendingValue = &pending
			response.RestartRequired = true
		}
		response.Item = append(response.Item, dto)
	}
	return response, nil
}

// isUser reports whether the requester is a signed-in person rather than an API client.
func isUser(requester identity.Requester) bool {
	return requester.Role == identity.RoleAdmin || requester.Role == identity.RoleUser
}
