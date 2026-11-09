// Package users lets administrators create and manage accounts and see what each one holds.
package users

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/id"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// PlatformCount is how many of a user's ROMs carry a platform.
type PlatformCount struct {
	Platform string
	Count    uint64
}

// Activity summarizes what a user holds.
type Activity struct {
	ROMCount       uint64
	StorageBytes   uint64
	UploadCount    uint64
	ActiveSessions uint64
	Platforms      []PlatformCount
}

// Transfer reports what moved to the administrator when a user was deleted.
type Transfer struct {
	ROMs      uint64
	Libraries uint64
	Uploads   uint64
	BIOS      uint64
}

type Store interface {
	ListUsers(context.Context) ([]authdomain.User, error)
	FindByID(context.Context, string) (*authdomain.User, error)
	FindByEmail(context.Context, string) (*authdomain.User, error)
	FindByUsername(context.Context, string) (*authdomain.User, error)
	SaveUser(context.Context, authdomain.User) error
	// UserActivity summarizes every user's ROMs, uploads, and active sessions, keyed by user ID.
	UserActivity(context.Context) (map[string]Activity, error)
	// DeleteUserTransferringData deletes userID in one transaction, handing their ROMs, libraries,
	// and uploads to heirID; their API clients and sessions are deleted with them.
	DeleteUserTransferringData(ctx context.Context, userID, heirID string) (Transfer, error)
}

type Sessions interface {
	EndSessions(ctx context.Context, userID string) error
}

type PlatformDTO struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	ROMCount uint64 `json:"rom_count"`
}

type UserDTO struct {
	ID             string        `json:"id"`
	Email          string        `json:"email"`
	Username       *string       `json:"username"`
	Role           string        `json:"role"`
	ROMCount       uint64        `json:"rom_count"`
	StorageBytes   uint64        `json:"storage_bytes"`
	UploadCount    uint64        `json:"upload_count"`
	ActiveSessions uint64        `json:"active_sessions"`
	Platforms      []PlatformDTO `json:"platforms"`
	// IsSelf marks the requesting administrator, who cannot delete or demote themselves.
	IsSelf bool `json:"is_self"`
}

type ListResponse struct {
	Item []UserDTO `json:"item"`
}

type CreateRequest struct {
	Email    string  `json:"email"`
	Username *string `json:"username"`
	Password string  `json:"password"`
	Role     string  `json:"role"`
}

// UpdateRequest changes only the fields present. An empty username clears it; a password resets
// the user's password and signs them out everywhere.
type UpdateRequest struct {
	Email    *string `json:"email"`
	Username *string `json:"username"`
	Role     *string `json:"role"`
	Password *string `json:"password"`
}

type DeleteResponse struct {
	TransferredROMs      uint64 `json:"transferred_roms"`
	TransferredLibraries uint64 `json:"transferred_libraries"`
	TransferredUploads   uint64 `json:"transferred_uploads"`
	TransferredBIOS      uint64 `json:"transferred_bios"`
}

type API interface {
	List(context.Context, identity.Requester) (ListResponse, error)
	Create(context.Context, identity.Requester, CreateRequest) (UserDTO, error)
	Update(context.Context, identity.Requester, string, UpdateRequest) (UserDTO, error)
	Delete(context.Context, identity.Requester, string) (DeleteResponse, error)
	EndSessions(context.Context, identity.Requester, string) error
}

type Service struct {
	store    Store
	hasher   auth.PasswordHasher
	sessions Sessions
}

var _ API = (*Service)(nil)

func NewService(store Store, hasher auth.PasswordHasher, sessions Sessions) *Service {
	return &Service{store: store, hasher: hasher, sessions: sessions}
}

func (service *Service) List(ctx context.Context, requester identity.Requester) (ListResponse, error) {
	if !requester.IsAdmin() {
		return ListResponse{}, fault.Forbidden()
	}
	accounts, err := service.store.ListUsers(ctx)
	if err != nil {
		return ListResponse{}, err
	}
	activity, err := service.store.UserActivity(ctx)
	if err != nil {
		return ListResponse{}, err
	}
	response := ListResponse{Item: make([]UserDTO, 0, len(accounts))}
	for _, account := range accounts {
		response.Item = append(response.Item, userDTO(account, activity[account.ID], requester))
	}
	return response, nil
}

func (service *Service) Create(ctx context.Context, requester identity.Requester, request CreateRequest) (UserDTO, error) {
	if !requester.IsAdmin() {
		return UserDTO{}, fault.Forbidden()
	}
	email := strings.TrimSpace(request.Email)
	if err := service.checkEmail(ctx, email, ""); err != nil {
		return UserDTO{}, err
	}
	username, err := service.checkUsername(ctx, request.Username, "")
	if err != nil {
		return UserDTO{}, err
	}
	role, err := parseRole(request.Role)
	if err != nil {
		return UserDTO{}, err
	}
	hash, err := service.hashPassword(request.Password)
	if err != nil {
		return UserDTO{}, err
	}
	userID, err := id.New("usr")
	if err != nil {
		return UserDTO{}, fault.Internal(fmt.Errorf("generating user id: %w", err))
	}
	account := authdomain.User{ID: userID, Email: email, Username: username, PasswordHash: hash, Role: role}
	if err := service.store.SaveUser(ctx, account); err != nil {
		return UserDTO{}, err
	}
	return userDTO(account, Activity{}, requester), nil
}

func (service *Service) Update(ctx context.Context, requester identity.Requester, userID string, request UpdateRequest) (UserDTO, error) {
	account, err := service.adminTarget(ctx, requester, userID)
	if err != nil {
		return UserDTO{}, err
	}
	if request.Email != nil {
		email := strings.TrimSpace(*request.Email)
		if err := service.checkEmail(ctx, email, account.ID); err != nil {
			return UserDTO{}, err
		}
		account.Email = email
	}
	if request.Username != nil {
		if account.Username, err = service.checkUsername(ctx, request.Username, account.ID); err != nil {
			return UserDTO{}, err
		}
	}
	if request.Role != nil {
		role, err := parseRole(*request.Role)
		if err != nil {
			return UserDTO{}, err
		}
		// Administrators cannot demote themselves, so at least one administrator always remains.
		if account.ID == requester.UserID && role != identity.RoleAdmin {
			return UserDTO{}, fault.Validation("you cannot remove your own administrator role")
		}
		account.Role = role
	}
	passwordReset := request.Password != nil
	if passwordReset {
		if account.PasswordHash, err = service.hashPassword(*request.Password); err != nil {
			return UserDTO{}, err
		}
	}
	if err := service.store.SaveUser(ctx, *account); err != nil {
		return UserDTO{}, err
	}
	// A reset password must lock out whoever knew the old one.
	if passwordReset && account.ID != requester.UserID {
		if err := service.sessions.EndSessions(ctx, account.ID); err != nil {
			return UserDTO{}, err
		}
	}
	return service.summary(ctx, *account, requester)
}

// Delete removes a user, handing their ROMs, libraries, and uploads to the requesting
// administrator so nothing they uploaded is lost.
func (service *Service) Delete(ctx context.Context, requester identity.Requester, userID string) (DeleteResponse, error) {
	account, err := service.adminTarget(ctx, requester, userID)
	if err != nil {
		return DeleteResponse{}, err
	}
	if account.ID == requester.UserID {
		return DeleteResponse{}, fault.Validation("you cannot delete your own account")
	}
	transfer, err := service.store.DeleteUserTransferringData(ctx, account.ID, requester.UserID)
	if err != nil {
		return DeleteResponse{}, err
	}
	return DeleteResponse{TransferredROMs: transfer.ROMs, TransferredLibraries: transfer.Libraries, TransferredUploads: transfer.Uploads, TransferredBIOS: transfer.BIOS}, nil
}

// EndSessions signs a user out on every device.
func (service *Service) EndSessions(ctx context.Context, requester identity.Requester, userID string) error {
	account, err := service.adminTarget(ctx, requester, userID)
	if err != nil {
		return err
	}
	return service.sessions.EndSessions(ctx, account.ID)
}

func (service *Service) adminTarget(ctx context.Context, requester identity.Requester, userID string) (*authdomain.User, error) {
	if !requester.IsAdmin() {
		return nil, fault.Forbidden()
	}
	account, err := service.store.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fault.NotFound()
	}
	return account, nil
}

func (service *Service) summary(ctx context.Context, account authdomain.User, requester identity.Requester) (UserDTO, error) {
	activity, err := service.store.UserActivity(ctx)
	if err != nil {
		return UserDTO{}, err
	}
	return userDTO(account, activity[account.ID], requester), nil
}

func (service *Service) checkEmail(ctx context.Context, email, userID string) error {
	if err := auth.ValidateEmail(email); err != nil {
		return err
	}
	existing, err := service.store.FindByEmail(ctx, email)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != userID {
		return &fault.Error{Code: fault.CodeConflict, Message: "email is already in use"}
	}
	return nil
}

func (service *Service) checkUsername(ctx context.Context, raw *string, userID string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	username := auth.NormalizeUsername(*raw)
	if username == "" {
		return nil, nil
	}
	if err := auth.ValidateUsername(username); err != nil {
		return nil, err
	}
	existing, err := service.store.FindByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.ID != userID {
		return nil, &fault.Error{Code: fault.CodeConflict, Message: "username is already in use"}
	}
	return &username, nil
}

func (service *Service) hashPassword(password string) (string, error) {
	if len(password) < auth.MinPasswordLength {
		return "", fault.Validation(fmt.Sprintf("password must be at least %d characters", auth.MinPasswordLength))
	}
	hash, err := service.hasher.Hash(password)
	if err != nil {
		return "", fault.Internal(err)
	}
	return hash, nil
}

func parseRole(raw string) (identity.RequesterRole, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case RoleAdmin:
		return identity.RoleAdmin, nil
	case RoleUser, "":
		return identity.RoleUser, nil
	default:
		return identity.RoleUnknown, fault.Validation("role must be admin or user")
	}
}

func roleName(role identity.RequesterRole) string {
	if role == identity.RoleAdmin {
		return RoleAdmin
	}
	return RoleUser
}

func userDTO(account authdomain.User, activity Activity, requester identity.Requester) UserDTO {
	platforms := make([]PlatformDTO, 0, len(activity.Platforms))
	for _, platform := range activity.Platforms {
		platforms = append(platforms, PlatformDTO{Key: platform.Platform, Name: romdomain.PlatformName(platform.Platform), ROMCount: platform.Count})
	}
	// Most-used platforms first.
	slices.SortFunc(platforms, func(a, b PlatformDTO) int {
		if order := cmp.Compare(b.ROMCount, a.ROMCount); order != 0 {
			return order
		}
		return strings.Compare(a.Name, b.Name)
	})
	return UserDTO{
		ID: account.ID, Email: account.Email, Username: account.Username, Role: roleName(account.Role),
		ROMCount: activity.ROMCount, StorageBytes: activity.StorageBytes, UploadCount: activity.UploadCount,
		ActiveSessions: activity.ActiveSessions, Platforms: platforms, IsSelf: account.ID == requester.UserID,
	}
}
