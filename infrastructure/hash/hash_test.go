package hash

import (
	"testing"

	romdomain "github.com/lcmps/retroxide/domain/rom"
)

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

func TestParallelDigestMatchesSequentialForAnyWriteSizes(t *testing.T) {
	content := make([]byte, 5*blockSize+12345)
	for index := range content {
		content[index] = byte(index*31 + index>>9)
	}
	want, err := sequentialSum(content)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{1, 7, 32 << 10, blockSize - 1, blockSize, blockSize + 1, 3 * blockSize, len(content)} {
		digest := New().NewDigest()
		for offset := 0; offset < len(content); offset += size {
			// Reusing the caller's buffer after Write returns must not change the result.
			chunk := append([]byte(nil), content[offset:min(offset+size, len(content))]...)
			if _, err := digest.Write(chunk); err != nil {
				t.Fatal(err)
			}
			clear(chunk)
		}
		got, err := digest.Sum()
		if err != nil || got != want {
			t.Fatalf("writes of %d bytes: %+v, %v; want %+v", size, got, err, want)
		}
	}
	for _, content := range [][]byte{nil, []byte("abc"), content} {
		want, _ := sequentialSum(content)
		if got, err := New().Hash(content); err != nil || got != want {
			t.Fatalf("Hash(%d bytes) = %+v, %v; want %+v", len(content), got, err, want)
		}
	}
}

func sequentialSum(content []byte) (romdomain.ROMHash, error) {
	digest := newSequentialDigest()
	_, _ = digest.Write(content)
	return digest.Sum()
}

func TestResumedDigestMatchesOneUninterruptedPass(t *testing.T) {
	content := make([]byte, 3*blockSize+777)
	for index := range content {
		content[index] = byte(index * 13)
	}
	want, _ := sequentialSum(content)
	service := New()
	digest, err := service.ResumeDigest(nil)
	if err != nil {
		t.Fatal(err)
	}
	// Save after each uneven part, then continue in a fresh digest, as across requests.
	for offset, part := 0, 0; offset < len(content); part++ {
		end := min(offset+(part%3+1)*400_000+17, len(content))
		if _, err := digest.Write(content[offset:end]); err != nil {
			t.Fatal(err)
		}
		offset = end
		state, err := digest.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if digest, err = service.ResumeDigest(state); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := digest.Sum(); err != nil || got != want {
		t.Fatalf("resumed digest = %+v, %v; want %+v", got, err, want)
	}
	for _, state := range [][]byte{{9}, {digestStateVersion, 200}, append([]byte{digestStateVersion}, make([]byte, 3)...)} {
		if _, err := service.ResumeDigest(state); err == nil {
			t.Errorf("ResumeDigest(%v) accepted corrupt progress", state)
		}
	}
}
