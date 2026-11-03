package users

import (
	"context"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
)

type fakeStore struct {
	accounts map[string]authdomain.User
	activity map[string]Activity
	deleted  []string
	heir     string
}

func (store *fakeStore) ListUsers(context.Context) ([]authdomain.User, error) {
	list := make([]authdomain.User, 0, len(store.accounts))
	for _, account := range store.accounts {
		list = append(list, account)
	}
	return list, nil
}

func (store *fakeStore) FindByID(_ context.Context, userID string) (*authdomain.User, error) {
	if account, ok := store.accounts[userID]; ok {
		return &account, nil
	}
	return nil, nil
}

func (store *fakeStore) FindByEmail(_ context.Context, email string) (*authdomain.User, error) {
	for _, account := range store.accounts {
		if account.Email == email {
			return &account, nil
		}
	}
	return nil, nil
}

func (store *fakeStore) FindByUsername(_ context.Context, username string) (*authdomain.User, error) {
	for _, account := range store.accounts {
		if account.Username != nil && *account.Username == username {
			return &account, nil
		}
	}
	return nil, nil
}

func (store *fakeStore) SaveUser(_ context.Context, account authdomain.User) error {
	store.accounts[account.ID] = account
	return nil
}

func (store *fakeStore) UserActivity(context.Context) (map[string]Activity, error) {
	return store.activity, nil
}

func (store *fakeStore) DeleteUserTransferringData(_ context.Context, userID, heirID string) (Transfer, error) {
	store.deleted = append(store.deleted, userID)
	store.heir = heirID
	delete(store.accounts, userID)
	return Transfer{ROMs: store.activity[userID].ROMCount}, nil
}

type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error)       { return "hashed:" + password, nil }
func (fakeHasher) Verify(password, hash string) (bool, error) { return hash == "hashed:"+password, nil }

type fakeSessions struct{ ended []string }

func (sessions *fakeSessions) EndSessions(_ context.Context, userID string) error {
	sessions.ended = append(sessions.ended, userID)
	return nil
}

var (
	admin  = identity.Requester{UserID: "usr-admin", Role: identity.RoleAdmin}
	member = identity.Requester{UserID: "usr-member", Role: identity.RoleUser}
	client = identity.Requester{UserID: "usr-admin", Role: identity.RoleClient}
)

func fixture() (*Service, *fakeStore, *fakeSessions) {
	store := &fakeStore{
		accounts: map[string]authdomain.User{
			"usr-admin":  {ID: "usr-admin", Email: "admin@example.com", Role: identity.RoleAdmin},
			"usr-member": {ID: "usr-member", Email: "member@example.com", Role: identity.RoleUser},
		},
		activity: map[string]Activity{"usr-member": {
			ROMCount: 3, StorageBytes: 4096, UploadCount: 4, ActiveSessions: 1,
			Platforms: []PlatformCount{{Platform: "nes", Count: 1}, {Platform: "sms", Count: 2}},
		}},
	}
	sessions := &fakeSessions{}
	return NewService(store, fakeHasher{}, sessions), store, sessions
}

func ptr(value string) *string { return &value }

func TestOnlyAdministratorsManageUsers(t *testing.T) {
	service, _, _ := fixture()
	ctx := context.Background()
	for _, requester := range []identity.Requester{member, client} {
		if _, err := service.List(ctx, requester); !fault.IsCode(err, fault.CodeForbidden) {
			t.Errorf("List(%v) = %v, want forbidden", requester.Role, err)
		}
		if _, err := service.Create(ctx, requester, CreateRequest{Email: "x@example.com", Password: "long enough"}); !fault.IsCode(err, fault.CodeForbidden) {
			t.Errorf("Create(%v) = %v, want forbidden", requester.Role, err)
		}
		if _, err := service.Delete(ctx, requester, "usr-member"); !fault.IsCode(err, fault.CodeForbidden) {
			t.Errorf("Delete(%v) = %v, want forbidden", requester.Role, err)
		}
	}
}

func TestListIncludesActivityWithMostUsedPlatformsFirst(t *testing.T) {
	service, _, _ := fixture()
	list, err := service.List(context.Background(), admin)
	if err != nil {
		t.Fatal(err)
	}
	var found *UserDTO
	for index := range list.Item {
		if list.Item[index].ID == "usr-member" {
			found = &list.Item[index]
		}
		if list.Item[index].ID == "usr-admin" && !list.Item[index].IsSelf {
			t.Error("the requesting administrator should be marked as self")
		}
	}
	if found == nil || found.ROMCount != 3 || found.StorageBytes != 4096 || found.UploadCount != 4 || found.ActiveSessions != 1 {
		t.Fatalf("member summary = %+v", found)
	}
	if len(found.Platforms) != 2 || found.Platforms[0].Name != "Master System" || found.Platforms[0].ROMCount != 2 {
		t.Fatalf("platforms = %+v, want Master System (2) first", found.Platforms)
	}
}

func TestCreateValidatesAndRejectsDuplicates(t *testing.T) {
	service, store, _ := fixture()
	ctx := context.Background()
	created, err := service.Create(ctx, admin, CreateRequest{Email: " new@example.com ", Username: ptr(" New.Player "), Password: "a fine password", Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	saved := store.accounts[created.ID]
	if created.Email != "new@example.com" || created.Username == nil || *created.Username != "new.player" || saved.PasswordHash != "hashed:a fine password" || saved.Role != identity.RoleUser {
		t.Fatalf("created = %+v, saved = %+v", created, saved)
	}
	for name, request := range map[string]CreateRequest{
		"taken email":     {Email: "member@example.com", Password: "a fine password"},
		"taken username":  {Email: "other@example.com", Username: ptr("NEW.PLAYER"), Password: "a fine password"},
		"short password":  {Email: "other@example.com", Password: "short"},
		"bad username":    {Email: "other@example.com", Username: ptr("no spaces"), Password: "a fine password"},
		"username with @": {Email: "other@example.com", Username: ptr("me@home"), Password: "a fine password"},
		"unknown role":    {Email: "other@example.com", Password: "a fine password", Role: "owner"},
		"invalid email":   {Email: "not-an-email", Password: "a fine password"},
	} {
		if _, err := service.Create(ctx, admin, request); err == nil {
			t.Errorf("%s: Create() succeeded", name)
		}
	}
}

func TestUpdateProtectsTheAdministratorAndResetsSessions(t *testing.T) {
	service, store, sessions := fixture()
	ctx := context.Background()
	if _, err := service.Update(ctx, admin, "usr-admin", UpdateRequest{Role: ptr("user")}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("demoting yourself = %v, want validation error", err)
	}
	updated, err := service.Update(ctx, admin, "usr-member", UpdateRequest{Role: ptr("admin"), Username: ptr("player1"), Password: ptr("brand new password")})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Role != "admin" || *updated.Username != "player1" || store.accounts["usr-member"].PasswordHash != "hashed:brand new password" {
		t.Fatalf("updated = %+v", updated)
	}
	if len(sessions.ended) != 1 || sessions.ended[0] != "usr-member" {
		t.Fatalf("ended sessions = %v, want the member signed out after a password reset", sessions.ended)
	}
	if cleared, err := service.Update(ctx, admin, "usr-member", UpdateRequest{Username: ptr("")}); err != nil || cleared.Username != nil {
		t.Fatalf("clearing the username = %+v, %v", cleared, err)
	}
	if _, err := service.Update(ctx, admin, "usr-missing", UpdateRequest{Role: ptr("user")}); !fault.IsCode(err, fault.CodeNotFound) {
		t.Fatalf("unknown user = %v, want not found", err)
	}
}

func TestDeleteTransfersToTheAdministratorAndNeverDeletesSelf(t *testing.T) {
	service, store, _ := fixture()
	ctx := context.Background()
	if _, err := service.Delete(ctx, admin, "usr-admin"); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("deleting yourself = %v, want validation error", err)
	}
	response, err := service.Delete(ctx, admin, "usr-member")
	if err != nil {
		t.Fatal(err)
	}
	if response.TransferredROMs != 3 || store.heir != "usr-admin" || len(store.deleted) != 1 {
		t.Fatalf("delete = %+v, heir %q, deleted %v", response, store.heir, store.deleted)
	}
}
