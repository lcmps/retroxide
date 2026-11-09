package bios

import (
	"testing"

	"github.com/lcmps/retroxide/domain/identity"
)

func TestCanModify(t *testing.T) {
	entry := BIOS{UploaderID: "usr_owner"}
	cases := []struct {
		name      string
		requester identity.Requester
		want      bool
	}{
		{"admin", identity.Requester{UserID: "usr_admin", Role: identity.RoleAdmin}, true},
		{"owner", identity.Requester{UserID: "usr_owner", Role: identity.RoleUser}, true},
		{"other user", identity.Requester{UserID: "usr_other", Role: identity.RoleUser}, false},
		// Client tokens read BIOS files but never change them, even their own user's.
		{"owner's client", identity.Requester{UserID: "usr_owner", Role: identity.RoleClient}, false},
	}
	for _, test := range cases {
		if got := CanModify(test.requester, entry); got != test.want {
			t.Errorf("%s: CanModify = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestDownloadName(t *testing.T) {
	path := "pcsx2/bios/ps2-0160e-20010704.bin"
	if got := (BIOS{Filename: "upload.bin", ExpectedPath: &path}).DownloadName(); got != "ps2-0160e-20010704.bin" {
		t.Errorf("recognized DownloadName = %q", got)
	}
	if got := (BIOS{Filename: "upload.bin"}).DownloadName(); got != "upload.bin" {
		t.Errorf("unrecognized DownloadName = %q", got)
	}
}
