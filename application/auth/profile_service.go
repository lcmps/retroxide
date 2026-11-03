package auth

import (
	"context"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/appearance"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
)

// MinPasswordLength matches the setup wizard's rule for the administrator password.
const MinPasswordLength = 8

type ProfileResponse struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	// Username is the optional sign-in name; null when the user signs in by email only.
	Username *string `json:"username"`
	Role     string  `json:"role"`
	// Locale is the chosen interface language; null follows the browser.
	Locale *string `json:"locale"`
	// Font is the chosen interface font; null uses the application default.
	Font *string `json:"font"`
}

// UpdateProfileRequest changes the email, password, language, and/or font. Email and password
// changes require CurrentPassword; preference changes do not. An empty Locale follows the browser
// and an empty Font uses the application default.
type UpdateProfileRequest struct {
	Email           *string `json:"email"`
	Username        *string `json:"username"`
	CurrentPassword string  `json:"current_password"`
	NewPassword     *string `json:"new_password"`
	Locale          *string `json:"locale"`
	Font            *string `json:"font"`
}

type UpdateProfileResponse struct {
	Profile ProfileResponse `json:"profile"`
	// Session replaces the caller's tokens after a password change, which ends every session.
	Session *SessionResponse `json:"session"`
}

type ProfileAPI interface {
	Profile(context.Context, identity.Requester) (ProfileResponse, error)
	UpdateProfile(context.Context, identity.Requester, UpdateProfileRequest) (UpdateProfileResponse, error)
}

var _ ProfileAPI = (*SessionService)(nil)

// Profile returns the signed-in user's account. API clients have no profile.
func (service *SessionService) Profile(ctx context.Context, requester identity.Requester) (ProfileResponse, error) {
	user, err := service.profileUser(ctx, requester)
	if err != nil {
		return ProfileResponse{}, err
	}
	return profileOf(*user)
}

// UpdateProfile changes the signed-in user's email and/or password after re-checking the current
// password. A new password signs out every session, including other devices, and returns a fresh
// session for the caller.
func (service *SessionService) UpdateProfile(ctx context.Context, requester identity.Requester, request UpdateProfileRequest) (UpdateProfileResponse, error) {
	user, err := service.profileUser(ctx, requester)
	if err != nil {
		return UpdateProfileResponse{}, err
	}
	if request.Email == nil && request.Username == nil && request.NewPassword == nil && request.Locale == nil && request.Font == nil {
		return UpdateProfileResponse{}, fault.Validation("nothing to update")
	}
	if request.Locale != nil {
		locale := strings.TrimSpace(*request.Locale)
		if locale == "" {
			user.Locale = nil
		} else if err := ValidateLocale(locale); err != nil {
			return UpdateProfileResponse{}, err
		} else {
			user.Locale = &locale
		}
	}
	if request.Font != nil {
		font := strings.ToLower(strings.TrimSpace(*request.Font))
		switch {
		case font == "":
			user.Font = nil
		case !appearance.IsFont(font):
			return UpdateProfileResponse{}, fault.Validation("font must be pixel, ubuntu, or system")
		default:
			user.Font = &font
		}
	}
	// Changing how the account signs in needs the current password; choosing preferences does not.
	if request.Email != nil || request.Username != nil || request.NewPassword != nil {
		valid, err := service.hasher.Verify(request.CurrentPassword, user.PasswordHash)
		if err != nil {
			return UpdateProfileResponse{}, fault.Internal(err)
		}
		if !valid {
			return UpdateProfileResponse{}, fault.Validation("current password is incorrect")
		}
	}

	if request.Email != nil {
		email := strings.TrimSpace(*request.Email)
		if err := ValidateEmail(email); err != nil {
			return UpdateProfileResponse{}, err
		}
		if !strings.EqualFold(email, user.Email) {
			existing, err := service.users.FindByEmail(ctx, email)
			if err != nil {
				return UpdateProfileResponse{}, err
			}
			if existing != nil && existing.ID != user.ID {
				return UpdateProfileResponse{}, &fault.Error{Code: fault.CodeConflict, Message: "email is already in use"}
			}
		}
		user.Email = email
	}
	if request.Username != nil {
		username, err := service.availableUsername(ctx, *request.Username, user.ID)
		if err != nil {
			return UpdateProfileResponse{}, err
		}
		user.Username = username
	}
	passwordChanged := false
	if request.NewPassword != nil {
		if len(*request.NewPassword) < MinPasswordLength {
			return UpdateProfileResponse{}, fault.Validation("new password must be at least 8 characters")
		}
		hash, err := service.hasher.Hash(*request.NewPassword)
		if err != nil {
			return UpdateProfileResponse{}, fault.Internal(err)
		}
		user.PasswordHash = hash
		passwordChanged = true
	}
	if err := service.users.SaveUser(ctx, *user); err != nil {
		return UpdateProfileResponse{}, err
	}

	profile, err := profileOf(*user)
	if err != nil {
		return UpdateProfileResponse{}, err
	}
	response := UpdateProfileResponse{Profile: profile}
	if passwordChanged {
		if err := service.endAllSessions(ctx, user.ID); err != nil {
			return UpdateProfileResponse{}, err
		}
		refreshed, err := service.users.FindByID(ctx, user.ID)
		if err != nil {
			return UpdateProfileResponse{}, err
		}
		if refreshed == nil {
			return UpdateProfileResponse{}, fault.Unauthorized()
		}
		session, err := service.issueSession(ctx, *refreshed)
		if err != nil {
			return UpdateProfileResponse{}, err
		}
		response.Session = &session
	}
	return response, nil
}

// availableUsername normalizes and validates a username for userID, rejecting one another user
// already has. An empty username clears it (nil).
func (service *SessionService) availableUsername(ctx context.Context, raw, userID string) (*string, error) {
	username := NormalizeUsername(raw)
	if username == "" {
		return nil, nil
	}
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	existing, err := service.users.FindByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.ID != userID {
		return nil, &fault.Error{Code: fault.CodeConflict, Message: "username is already in use"}
	}
	return &username, nil
}

func (service *SessionService) profileUser(ctx context.Context, requester identity.Requester) (*authdomain.User, error) {
	if requester.Role != identity.RoleAdmin && requester.Role != identity.RoleUser {
		return nil, fault.Forbidden()
	}
	user, err := service.users.FindByID(ctx, requester.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fault.Unauthorized()
	}
	return user, nil
}

func profileOf(user authdomain.User) (ProfileResponse, error) {
	role, err := roleString(user.Role)
	if err != nil {
		return ProfileResponse{}, err
	}
	return ProfileResponse{UserID: user.ID, Email: user.Email, Username: user.Username, Role: role, Locale: user.Locale, Font: user.Font}, nil
}
