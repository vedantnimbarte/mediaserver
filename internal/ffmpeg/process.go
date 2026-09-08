package ffmpeg

import (
	"os"
	"os/exec"
)

// PrepareCommand applies the platform-specific process settings every ffmpeg
// invocation needs: no console window on Windows, and a dedicated process group so
// the whole tree can be signalled at once.
func PrepareCommand(cmd *exec.Cmd) { hideWindow(cmd) }

// Supervise enrols a running process in the server's cleanup mechanism, so that an
// abnormal server exit does not leave orphaned encoders running.
func Supervise(p *os.Process) { superviseProcess(p) }

// KillTree terminates a process and any children it started.
func KillTree(p *os.Process) error { return killProcessTree(p) }
