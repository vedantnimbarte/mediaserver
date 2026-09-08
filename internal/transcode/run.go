package transcode

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"kino/internal/ffmpeg"
)

// maxStderrLines caps how much of ffmpeg's output we keep for the error message.
// ffmpeg can emit thousands of warnings on a damaged file; only the tail is useful.
const maxStderrLines = 20

// runFFmpeg executes one encoder process and blocks until it exits.
//
// Cancelling ctx kills the whole process tree, which is what actually stops a
// transcode when the viewer seeks or closes the tab.
func (s *Session) runFFmpeg(ctx context.Context, args []string, w *worker, dir string) error {
	cmd := exec.Command(s.tools.FFmpeg, args...)
	ffmpeg.PrepareCommand(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("attach stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	// Enrol the process so that if this server dies without unwinding, the OS still
	// reaps the encoder rather than leaving it pinning a core forever.
	ffmpeg.Supervise(cmd.Process)

	// Drain stderr continuously. If we did not, a chatty ffmpeg would fill the pipe
	// buffer and block forever mid-encode.
	tail := make([]string, 0, maxStderrLines)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if len(tail) == maxStderrLines {
				tail = tail[1:]
			}
			tail = append(tail, line)
		}
	}()

	// Kill the process when the context is cancelled. The watcher exits via `finished`
	// on the normal path, so only one goroutine ever closes that channel.
	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			ffmpeg.KillTree(cmd.Process)
		case <-finished:
		}
	}()

	waitErr := cmd.Wait()
	close(finished)
	<-drained

	if ctx.Err() != nil {
		return nil // cancelled on purpose: a seek or a closed session
	}
	if waitErr != nil {
		detail := strings.Join(tail, "; ")
		if detail == "" {
			detail = waitErr.Error()
		}
		return fmt.Errorf("ffmpeg exited: %s", detail)
	}

	log.Printf("transcode: session %s finished encoding %s", s.ID, dir)
	return nil
}
