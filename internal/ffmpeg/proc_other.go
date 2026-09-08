//go:build !windows

package ffmpeg

import (
	"os"
	"os/exec"
	"syscall"
)

// hideWindow is a no-op outside Windows, but it does put each child in its own
// process group so that killProcessTree can signal the whole group at once.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// superviseProcess is a no-op: on Unix the process group set above is enough, and a
// killed parent leaves children reparented to init rather than pinned to the CPU.
func superviseProcess(p *os.Process) {}

// killProcessTree signals the child's entire process group.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	// The negative PID targets the process group created by hideWindow.
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		return p.Kill()
	}
	return nil
}
