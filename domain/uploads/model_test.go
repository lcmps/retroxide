package uploads

import (
	"testing"

	"github.com/lcmps/retroxide/domain/identity"
)

func TestUploadTransitionTo(t *testing.T) {
	tests := []struct {
		name     string
		current  UploadStatus
		next     UploadStatus
		expected UploadStatus
		wantErr  bool
	}{
		{name: "pending to processing", current: UploadStatusPending, next: UploadStatusProcessing, expected: UploadStatusProcessing},
		{name: "pending to failed", current: UploadStatusPending, next: UploadStatusFailed, expected: UploadStatusFailed},
		{name: "processing to completed", current: UploadStatusProcessing, next: UploadStatusCompleted, expected: UploadStatusCompleted},
		{name: "processing to failed", current: UploadStatusProcessing, next: UploadStatusFailed, expected: UploadStatusFailed},
		{name: "failed retry", current: UploadStatusFailed, next: UploadStatusProcessing, expected: UploadStatusProcessing},
		{name: "completed is terminal", current: UploadStatusCompleted, next: UploadStatusProcessing, expected: UploadStatusCompleted, wantErr: true},
		{name: "skip transition rejected", current: UploadStatusPending, next: UploadStatusCompleted, expected: UploadStatusPending, wantErr: true},
		{name: "self transition rejected", current: UploadStatusProcessing, next: UploadStatusProcessing, expected: UploadStatusProcessing, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upload := Upload{Status: test.current}
			err := upload.TransitionTo(test.next)
			if (err != nil) != test.wantErr {
				t.Fatalf("TransitionTo(%q) error = %v, wantErr %t", test.next, err, test.wantErr)
			}
			if upload.Status != test.expected {
				t.Fatalf("status = %q, want %q", upload.Status, test.expected)
			}
		})
	}
}

func TestRequesterCanAccess(t *testing.T) {
	tests := []struct {
		name    string
		role    identity.RequesterRole
		userID  string
		ownerID string
		want    bool
	}{
		{name: "owner user", role: identity.RoleUser, userID: "user-1", ownerID: "user-1", want: true},
		{name: "other user", role: identity.RoleUser, userID: "user-1", ownerID: "user-2"},
		{name: "admin", role: identity.RoleAdmin, userID: "admin-1", ownerID: "user-2", want: true},
		{name: "client", role: identity.RoleClient, userID: "client-owner", ownerID: "user-2", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requester := identity.Requester{UserID: test.userID, Role: test.role}
			if got := requester.CanAccess(test.ownerID); got != test.want {
				t.Fatalf("CanAccess(%q) = %t, want %t", test.ownerID, got, test.want)
			}
		})
	}
}
