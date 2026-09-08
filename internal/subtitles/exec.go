package subtitles

import (
	"context"
	"os/exec"
)

// execCommand is a thin indirection so tests can stub out ffmpeg if needed.
var execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
