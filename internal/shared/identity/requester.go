package identity

type RequesterRole uint8

const (
	RoleUnknown RequesterRole = iota
	RoleAdmin
	RoleUser
	RoleClient
)

type Requester struct {
	UserID string
	Role   RequesterRole
}

func (requester Requester) IsAdmin() bool {
	return requester.Role == RoleAdmin
}

func (requester Requester) HasBroadAccess() bool {
	return requester.Role == RoleAdmin || requester.Role == RoleClient
}

func (requester Requester) Owns(ownerID string) bool {
	return requester.UserID == ownerID
}

func (requester Requester) CanAccess(ownerID string) bool {
	return requester.HasBroadAccess() || requester.Owns(ownerID)
}
