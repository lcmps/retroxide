package rom

import "testing"

func TestNewROMHash(t *testing.T) {
	tests := []struct {
		name    string
		sha256  string
		sha1    string
		md5     string
		crc32   string
		wantErr bool
	}{
		{
			name:   "valid hashes",
			sha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sha1:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			md5:    "cccccccccccccccccccccccccccccccc",
			crc32:  "deadbeef",
		},
		{
			name:    "invalid sha256 length",
			sha256:  "aa",
			sha1:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			md5:     "cccccccccccccccccccccccccccccccc",
			crc32:   "deadbeef",
			wantErr: true,
		},
		{
			name:    "non hexadecimal sha1",
			sha256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sha1:    "gggggggggggggggggggggggggggggggggggggggg",
			md5:     "cccccccccccccccccccccccccccccccc",
			crc32:   "deadbeef",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewROMHash(test.sha256, test.sha1, test.md5, test.crc32)
			if (err != nil) != test.wantErr {
				t.Fatalf("NewROMHash() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}
