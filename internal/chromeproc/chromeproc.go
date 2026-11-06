// Package chromeproc finds the processes running from an install's App
// folder. They hold Chrome's files open, so they must all exit before those
// files change.
package chromeproc

import (
	"errors"
	"os/exec"
	"path/filepath"
)

// ErrElevated is Close's answer when Chrome runs as administrator: Windows
// drops what Pitstop sends to the windows of a process above its own.
var ErrElevated = errors.New("Chrome runs as administrator, so Pitstop cannot close it")

// Start runs exe on its own: it outlives Pitstop.
func Start(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		if needsElevation(err) {
			return shellOpen(exe)
		}
		return err
	}
	return cmd.Process.Release()
}

type Process struct {
	PID uint32
	// Parent is the process that started it, which may have exited.
	Parent uint32
	Path   string
}
