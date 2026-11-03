package hash

import (
	"bytes"
	"fmt"
	romdomain "github.com/lcmps/retroxide/domain/rom"
	"testing"
)

var benchmarkROMHash romdomain.ROMHash

func BenchmarkHash(b *testing.B) {
	service := New()
	for _, size := range []int{64 << 10, 1 << 20, 16 << 20} {
		b.Run(fmt.Sprintf("bytes=%d", size), func(b *testing.B) {
			content := bytes.Repeat([]byte("retroxide-benchmark"), size/len("retroxide-benchmark")+1)[:size]
			b.SetBytes(int64(size))
			b.ReportAllocs()
			var result romdomain.ROMHash
			for b.Loop() {
				var err error
				result, err = service.Hash(content)
				if err != nil {
					b.Fatal(err)
				}
			}
			benchmarkROMHash = result
		})
	}
}

// BenchmarkDigest streams content in 32 KiB writes, as an upload does, through the parallel
// digest and the single-pass sequential one it replaced.
func BenchmarkDigest(b *testing.B) {
	content := bytes.Repeat([]byte("retroxide-benchmark"), (64<<20)/len("retroxide-benchmark"))
	for _, variant := range []struct {
		name string
		new  func() romdomain.Digest
	}{
		{"parallel", func() romdomain.Digest { return newParallelDigest() }},
		{"sequential", func() romdomain.Digest { return newSequentialDigest() }},
	} {
		b.Run(variant.name, func(b *testing.B) {
			b.SetBytes(int64(len(content)))
			for b.Loop() {
				digest := variant.new()
				for offset := 0; offset < len(content); offset += 32 << 10 {
					_, _ = digest.Write(content[offset:min(offset+32<<10, len(content))])
				}
				result, err := digest.Sum()
				if err != nil {
					b.Fatal(err)
				}
				benchmarkROMHash = result
			}
		})
	}
}
