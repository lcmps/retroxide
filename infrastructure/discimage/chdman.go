package discimage

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Chdman compresses discs with MAME's chdman tool.
type Chdman struct {
	path    string
	threads int
}

// FindChdman returns chdman at path (a file or a name looked up in PATH), or false when it
// cannot be found or does not run.
func FindChdman(path string) (Chdman, bool) {
	if path == "" {
		path = "chdman"
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return Chdman{}, false
	}
	// chdman prints its usage, and its version, when run without arguments.
	output, _ := exec.Command(resolved).CombinedOutput()
	if !bytes.Contains(output, []byte("chdman")) {
		return Chdman{}, false
	}
	// Leave a core free so the server stays responsive while a disc compresses.
	return Chdman{path: resolved, threads: max(1, runtime.NumCPU()-1)}, true
}

// Path is where chdman was found.
func (chdman Chdman) Path() string { return chdman.path }

// Compress makes a CHD with chdman's default CD codecs (LZMA, deflate, and FLAC for audio).
func (chdman Chdman) Compress(ctx context.Context, sheetPath, output string) error {
	command := exec.CommandContext(ctx, chdman.path, "createcd", "-i", sheetPath, "-o", output, "-np", strconv.Itoa(chdman.threads))
	var messages bytes.Buffer
	command.Stdout, command.Stderr = &messages, &messages
	if err := command.Run(); err != nil {
		lines := strings.Split(strings.TrimSpace(messages.String()), "\n")
		return fmt.Errorf("chdman createcd: %w: %s", err, strings.TrimSpace(lines[len(lines)-1]))
	}
	return nil
}
