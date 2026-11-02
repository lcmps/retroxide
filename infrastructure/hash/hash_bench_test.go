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
