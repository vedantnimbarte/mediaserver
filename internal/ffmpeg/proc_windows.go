//go:build windows

package ffmpeg

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// hideWindow stops each ffmpeg/ffprobe invocation from flashing a console window.
// Without this a library scan pops hundreds of black boxes across the user's screen.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
)

// ensureJob lazily creates a job object configured to kill everything inside it when
// the last handle closes, which happens when this process exits for any reason.
//
// This is the safety net for the failure mode that bites hardest on Windows: the
// server crashing or being killed and leaving orphaned ffmpeg processes pinning the
// CPU and holding media files open until the user reboots.
func ensureJob() windows.Handle {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			log.Printf("ffmpeg: could not create job object, orphan protection disabled: %v", err)
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(
			h,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			log.Printf("ffmpeg: could not configure job object: %v", err)
			windows.CloseHandle(h)
			return
		}
		jobHandle = h
	})
	return jobHandle
}

// superviseProcess enrolls a freshly started process in the kill-on-close job.
func superviseProcess(p *os.Process) {
	if p == nil {
		return
	}
	job := ensureJob()
	if job == 0 {
		return
	}

	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(p.Pid),
	)
	if err != nil {
		return // the process may have already exited; nothing to supervise
	}
	defer windows.CloseHandle(h)

	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		log.Printf("ffmpeg: could not assign pid %d to job object: %v", p.Pid, err)
	}
}

// killProcessTree terminates a process and any children it spawned.
//
// os.Process.Kill only terminates the process itself. ffmpeg is normally a single
// process, but killing the tree is cheap insurance against a build that shells out.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid))
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		// taskkill fails if the process is already gone, which is a success for us.
		return p.Kill()
	}
	return nil
}
