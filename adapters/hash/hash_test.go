package hash

import "testing"

func TestServiceHash(t *testing.T) {
	got, err := New().Hash([]byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	want := struct {
		sha256 string
		sha1   string
		md5    string
		crc32  string
	}{
		sha256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		sha1:   "a9993e364706816aba3e25717850c26c9cd0d89d",
		md5:    "900150983cd24fb0d6963f7d28e17f72",
		crc32:  "352441c2",
	}
	if got.SHA256 != want.sha256 || got.SHA1 != want.sha1 || got.MD5 != want.md5 || got.CRC32 != want.crc32 {
		t.Fatalf("Hash(abc) = %+v, want %s/%s/%s/%s", got, want.sha256, want.sha1, want.md5, want.crc32)
	}
}
