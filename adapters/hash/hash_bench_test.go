package hash

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/lcmps/retroxide/internal/features/rom"
)

var benchmarkROMHash rom.ROMHash

func BenchmarkHash(b *testing.B) {
	service := New()
	for _, size := range []int{64 << 10, 1 << 20, 16 << 20} {
		b.Run(fmt.Sprintf("bytes=%d", size), func(b *testing.B) {
			content := bytes.Repeat([]byte("retroxide-benchmark"), size/len("retroxide-benchmark")+1)[:size]
			b.SetBytes(int64(size))
			b.ReportAllocs()
			var result rom.ROMHash
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
