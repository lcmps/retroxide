package discimage

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"

	chdman "github.com/lcmps/chdman-go"

	"github.com/lcmps/retroxide/application/discs"
)

// Compressor makes CHD images with the chdman-go library, so nothing has to be installed.
type Compressor struct {
	createCD func(ctx context.Context, cuePath, outPath string, options chdman.CreateOptions) (chdman.CreateResult, error)
}

var _ discs.Compressor = Compressor{}

// NewCompressor returns a compressor using chdman's default CD codecs (LZMA, deflate, and FLAC
// for audio).
func NewCompressor() Compressor {
	return Compressor{createCD: chdman.CreateCD}
}

// Compress makes a CHD at output from the CUE sheet at sheetPath and the track files beside it.
// progress, if set, is told how many bytes of the disc are done. The library runs inside the
// server, so a panic in it is returned as an error.
func (compressor Compressor) Compress(ctx context.Context, sheetPath, output string, progress func(done, total uint64)) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("chd compression panicked: %v", value)
		}
	}()
	result, err := compressor.createCD(ctx, sheetPath, output, chdman.CreateOptions{Workers: compressionWorkers(runtime.GOMAXPROCS(0)), Progress: progress})
	if err != nil {
		return fmt.Errorf("createcd: %w", err)
	}
	for _, warning := range result.Warnings {
		slog.WarnContext(ctx, "chd compression warning", "kind", warning.Kind, "track", warning.Track, "message", warning.Message)
	}
	return nil
}

// compressionWorkers leaves a core free so the server stays responsive while a disc compresses.
// It follows GOMAXPROCS rather than the CPU count, which ignores a container's CPU limit.
func compressionWorkers(procs int) int {
	return max(1, procs-1)
}
