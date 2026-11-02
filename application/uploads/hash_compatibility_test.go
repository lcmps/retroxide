package uploads

import (
	"context"
	"strings"
	"testing"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/domain/identity"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	romhash "github.com/lcmps/retroxide/infrastructure/hash"
)

func TestUploadServiceDuplicateHashCompatibility(t *testing.T) {
	hasher := romhash.New()
	incoming, err := hasher.Hash([]byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*romdomain.ROMHash)
		code   fault.Code
	}{
		{"legacy unknown CRC", func(hash *romdomain.ROMHash) { hash.CRC32 = "00000000" }, fault.CodeROMAlreadyExists},
		{"uppercase hashes", func(hash *romdomain.ROMHash) {
			hash.SHA256 = strings.ToUpper(hash.SHA256)
			hash.SHA1 = strings.ToUpper(hash.SHA1)
			hash.MD5 = strings.ToUpper(hash.MD5)
			hash.CRC32 = strings.ToUpper(hash.CRC32)
		}, fault.CodeROMAlreadyExists},
		{"different SHA1", func(hash *romdomain.ROMHash) { hash.SHA1 = strings.Repeat("a", 40) }, fault.CodeInternal},
		{"different MD5", func(hash *romdomain.ROMHash) { hash.MD5 = strings.Repeat("a", 32) }, fault.CodeInternal},
		{"different known CRC", func(hash *romdomain.ROMHash) { hash.CRC32 = "deadbeef" }, fault.CodeInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			stored := incoming
			test.mutate(&stored)
			storage := &testStorage{}
			queue := &testQueue{}
			service := NewUploadService(
				&testROMRepository{existing: &romdomain.ROMFile{ID: "rf-existing", Hash: stored}},
				&testUploadRepository{}, storage, hasher, queue, nil,
				UploadPolicy{MaxUploadBytes: 1024, AllowedExtensions: []string{"sfc"}},
			)
			_, err := service.Create(context.Background(), identity.Requester{UserID: "user-1", Role: identity.RoleUser}, UploadInput{
				Filename: "game.sfc", Content: strings.NewReader("abc"),
			})
			if !fault.IsCode(err, test.code) {
				t.Fatalf("Create() error = %v, want %s", err, test.code)
			}
			if storage.writes != 0 || storage.pending() != 0 || len(queue.payloads) != 0 {
				t.Fatal("existing content must not be written or enqueued again")
			}
		})
	}
}
