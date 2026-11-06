// Package chromeproc finds the processes running from an install's App
// folder. They hold Chrome's files open, so they must all exit before those
// files change.
package chromeproc

import (
	"os/exec"
	"path/filepath"
)

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
