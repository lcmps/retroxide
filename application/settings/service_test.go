package settings

import (
	"context"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
)

type fakeStore struct {
	settings []Setting
	updated  map[string]string
}

func (store *fakeStore) Settings() ([]Setting, error) { return store.settings, nil }

func (store *fakeStore) Update(values map[string]string) error {
	store.updated = values
	for index := range store.settings {
		if value, ok := values[store.settings[index].Key]; ok {
			store.settings[index].Configured = value
		}
	}
	return nil
}

func newFakeStore() *fakeStore {
	return &fakeStore{settings: []Setting{
		{Key: "UPLOAD", Value: "64MB", Configured: "64MB", Source: SourceDefault, Editable: true, Public: true},
		{Key: "STORAGE", Value: "/data", Configured: "/data", Source: SourceConfigFile},
		{Key: "PORT", Value: "3000", Configured: "3000", Source: SourceEnvironment, Editable: true},
		{Key: "ENV", Value: "prod", Configured: "prod", Source: SourceEnvironment, Public: true},
	}}
}

var (
	admin  = identity.Requester{UserID: "admin", Role: identity.RoleAdmin}
	member = identity.Requester{UserID: "member", Role: identity.RoleUser}
)

func TestListShowsPublicSettingsReadOnlyToUsersAndEverythingToAdmins(t *testing.T) {
	service := NewService(newFakeStore())

	forMember, err := service.List(context.Background(), member)
	if err != nil {
		t.Fatal(err)
	}
	if forMember.CanEdit || len(forMember.Item) != 2 || forMember.Item[0].Key != "UPLOAD" || forMember.Item[0].Editable {
		t.Fatalf("member view = %+v; want the public settings, read-only", forMember)
	}

	forAdmin, err := service.List(context.Background(), admin)
	if err != nil {
		t.Fatal(err)
	}
	editable := map[string]bool{}
	for _, item := range forAdmin.Item {
		editable[item.Key] = item.Editable
	}
	if !forAdmin.CanEdit || len(forAdmin.Item) != 4 || !editable["UPLOAD"] || editable["STORAGE"] || editable["PORT"] {
		t.Fatalf("admin view = %+v; want all settings, environment-set ones read-only", forAdmin)
	}
	if _, err := service.List(context.Background(), identity.Requester{UserID: "c", Role: identity.RoleClient}); !fault.IsCode(err, fault.CodeForbidden) {
		t.Fatalf("API client List() = %v, want forbidden", err)
	}
}

func TestUpdateIsAdminOnlyAndLimitedToEditableSettings(t *testing.T) {
	store := newFakeStore()
	service := NewService(store)
	ctx := context.Background()

	if _, err := service.Update(ctx, member, UpdateRequest{Values: map[string]string{"UPLOAD": "1024MB"}}); !fault.IsCode(err, fault.CodeForbidden) {
		t.Fatalf("member Update() = %v, want forbidden", err)
	}
	for key, want := range map[string]fault.Code{"STORAGE": fault.CodeValidation, "PORT": fault.CodeValidation, "NOPE": fault.CodeValidation} {
		if _, err := service.Update(ctx, admin, UpdateRequest{Values: map[string]string{key: "x"}}); !fault.IsCode(err, want) {
			t.Errorf("Update(%s) = %v, want %s", key, err, want)
		}
	}
	if store.updated != nil {
		t.Fatalf("rejected updates reached the store: %v", store.updated)
	}

	response, err := service.Update(ctx, admin, UpdateRequest{Values: map[string]string{"UPLOAD": "1024MB"}})
	if err != nil {
		t.Fatal(err)
	}
	if !response.RestartRequired || response.Item[0].PendingValue == nil || *response.Item[0].PendingValue != "1024MB" || response.Item[0].Value != "64MB" {
		t.Fatalf("Update() = %+v; want the change pending a restart", response)
	}
}

func TestLiveSettingsApplyImmediatelyAndAppearanceIsPublic(t *testing.T) {
	store := newFakeStore()
	store.settings = append(store.settings, Setting{
		Key: UIFontKey, Value: UIFontPixel, Configured: UIFontUbuntu, Source: SourceConfigFile,
		Editable: true, Public: true, Live: true,
		Options: []Option{{Value: UIFontPixel, Label: "Pixel"}, {Value: UIFontUbuntu, Label: "Ubuntu"}},
	})
	service := NewService(store)

	list, err := service.List(context.Background(), admin)
	if err != nil {
		t.Fatal(err)
	}
	font := list.Item[len(list.Item)-1]
	if font.Value != UIFontUbuntu || font.PendingValue != nil || list.RestartRequired || len(font.Options) != 2 {
		t.Fatalf("live font setting = %+v (restart %v); want the saved value, nothing pending", font, list.RestartRequired)
	}
	appearance, err := service.Appearance(context.Background())
	if err != nil || appearance.Font != UIFontUbuntu {
		t.Fatalf("Appearance() = %+v, %v", appearance, err)
	}
	if empty, _ := NewService(newFakeStore()).Appearance(context.Background()); empty.Font != UIFontPixel {
		t.Fatalf("Appearance() without the setting = %+v, want the pixel default", empty)
	}
}
