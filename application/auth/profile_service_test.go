package auth

import (
	"context"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
)

// profileTestUsers persists saved users, unlike the shared no-op fake.
type profileTestUsers struct{ authTestUserRepository }

func (users profileTestUsers) SaveUser(_ context.Context, user authdomain.User) error {
	for email, existing := range users.usersByEmail {
		if existing.ID == user.ID {
			delete(users.usersByEmail, email)
		}
	}
	users.usersByEmail[user.Email] = user
	users.usersByID[user.ID] = user
	return nil
}

func profileFixture() (*SessionService, profileTestUsers, *authTestRefreshRepository) {
	alice := authdomain.User{ID: "usr-alice", Email: "alice@example.com", PasswordHash: "hashed:old-password", Role: identity.RoleUser}
	bob := authdomain.User{ID: "usr-bob", Email: "bob@example.com", PasswordHash: "hashed:bob-password", Role: identity.RoleAdmin}
	users := profileTestUsers{authTestUserRepository{
		usersByEmail: map[string]authdomain.User{alice.Email: alice, bob.Email: bob},
		usersByID:    map[string]authdomain.User{alice.ID: alice, bob.ID: bob},
	}}
	refreshes := &authTestRefreshRepository{records: map[string]authdomain.RefreshToken{}}
	return NewSessionService(users, &authTestHasher{}, &authTestTokenService{}, refreshes), users, refreshes
}

var alice = identity.Requester{UserID: "usr-alice", Role: identity.RoleUser}

func ptr(value string) *string { return &value }

func TestProfileReturnsTheSignedInUserAndRejectsAPIClients(t *testing.T) {
	service, _, _ := profileFixture()
	profile, err := service.Profile(context.Background(), alice)
	if err != nil || profile != (ProfileResponse{UserID: "usr-alice", Email: "alice@example.com", Role: "user"}) {
		t.Fatalf("Profile() = %+v, %v", profile, err)
	}
	if _, err := service.Profile(context.Background(), identity.Requester{UserID: "usr-alice", Role: identity.RoleClient}); !fault.IsCode(err, fault.CodeForbidden) {
		t.Fatalf("Profile() for an API client = %v, want forbidden", err)
	}
}

func TestUpdateProfileChangesEmailOnlyWithTheCurrentPassword(t *testing.T) {
	service, users, refreshes := profileFixture()
	ctx := context.Background()

	if _, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Email: ptr("new@example.com"), CurrentPassword: "wrong"}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("wrong current password = %v, want validation error", err)
	}
	if _, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Email: ptr("bob@example.com"), CurrentPassword: "old-password"}); !fault.IsCode(err, fault.CodeConflict) {
		t.Fatalf("taken email = %v, want conflict", err)
	}
	if _, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Email: ptr("not-an-email"), CurrentPassword: "old-password"}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("invalid email = %v, want validation error", err)
	}

	updated, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Email: ptr(" new@example.com "), CurrentPassword: "old-password"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Profile.Email != "new@example.com" || updated.Session != nil {
		t.Fatalf("UpdateProfile() = %+v; want the new email and no new session", updated)
	}
	if users.usersByID["usr-alice"].Email != "new@example.com" || len(refreshes.revokedAll) != 0 {
		t.Fatalf("saved user = %+v, revoked sessions for %v", users.usersByID["usr-alice"], refreshes.revokedAll)
	}
}

func TestUpdateProfilePasswordChangeEndsOtherSessionsAndIssuesANewOne(t *testing.T) {
	service, users, refreshes := profileFixture()
	ctx := context.Background()

	if _, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{NewPassword: ptr("short"), CurrentPassword: "old-password"}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("short password = %v, want validation error", err)
	}
	updated, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{NewPassword: ptr("a much better password"), CurrentPassword: "old-password"})
	if err != nil {
		t.Fatal(err)
	}
	saved := users.usersByID["usr-alice"]
	if saved.PasswordHash != "hashed:a much better password" || saved.TokenVersion != 1 {
		t.Fatalf("saved user = %+v; want the new hash and a bumped token version", saved)
	}
	if len(refreshes.revokedAll) != 1 || updated.Session == nil || updated.Session.AccessToken == "" {
		t.Fatalf("revoked=%v session=%+v; want every session ended and a fresh one returned", refreshes.revokedAll, updated.Session)
	}
	if _, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{CurrentPassword: "a much better password"}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("empty update = %v, want validation error", err)
	}
}

func TestUpdateProfileSavesTheLanguageWithoutThePassword(t *testing.T) {
	service, users, _ := profileFixture()
	ctx := context.Background()

	updated, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Locale: ptr("pt-BR")})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Profile.Locale == nil || *updated.Profile.Locale != "pt-BR" || users.usersByID["usr-alice"].Locale == nil {
		t.Fatalf("UpdateProfile(locale) = %+v; want pt-BR saved", updated.Profile)
	}
	if profile, _ := service.Profile(ctx, alice); profile.Locale == nil || *profile.Locale != "pt-BR" {
		t.Fatalf("Profile().Locale = %v, want pt-BR", profile.Locale)
	}
	if _, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Locale: ptr("portuguese!")}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("malformed locale = %v, want validation error", err)
	}
	cleared, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Locale: ptr("")})
	if err != nil || cleared.Profile.Locale != nil || users.usersByID["usr-alice"].Locale != nil {
		t.Fatalf("clearing the locale = %+v, %v; want it to follow the browser again", cleared.Profile, err)
	}
	// An email change in the same request still needs the password, and saves nothing without it.
	if _, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Locale: ptr("en"), Email: ptr("x@example.com")}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("email change without password = %v, want validation error", err)
	}
	if users.usersByID["usr-alice"].Locale != nil {
		t.Fatal("a rejected request must not save its language change")
	}
}

func TestUpdateProfileSavesTheFontWithoutThePassword(t *testing.T) {
	service, users, _ := profileFixture()
	ctx := context.Background()

	updated, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Font: ptr("Ubuntu")})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Profile.Font == nil || *updated.Profile.Font != "ubuntu" || users.usersByID["usr-alice"].Font == nil {
		t.Fatalf("UpdateProfile(font) = %+v; want ubuntu saved", updated.Profile)
	}
	if _, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Font: ptr("comic-sans")}); !fault.IsCode(err, fault.CodeValidation) {
		t.Fatalf("unknown font = %v, want validation error", err)
	}
	cleared, err := service.UpdateProfile(ctx, alice, UpdateProfileRequest{Font: ptr("")})
	if err != nil || cleared.Profile.Font != nil || users.usersByID["usr-alice"].Font != nil {
		t.Fatalf("clearing the font = %+v, %v; want the application default again", cleared.Profile, err)
	}
}
