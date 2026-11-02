package chromeproc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for Chrome: copied into a folder
// and run with PITSTOP_WAIT=1, it waits until its stdin closes.
func TestMain(m *testing.M) {
	if os.Getenv("PITSTOP_WAIT") == "1" {
		io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRunning(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(t.TempDir(), "App")
	os.MkdirAll(app, 0o755)
	exe := filepath.Join(app, "chrome.exe")
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(exe, b, 0o755)

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "PITSTOP_WAIT=1")
	stdin, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ps, err := Running(app)
	if err != nil || len(ps) != 1 || int(ps[0].PID) != cmd.Process.Pid {
		t.Errorf("Running = %+v, %v; want pid %d", ps, err, cmd.Process.Pid)
	}
	// A sibling folder that shares the name's start does not count.
	if ps, _ := Running(app[:len(app)-1]); len(ps) != 0 {
		t.Errorf("Running(%s) = %+v", app[:len(app)-1], ps)
	}
	stdin.Close()
	cmd.Wait()
	if ps, err := Running(app); err != nil || len(ps) != 0 {
		t.Errorf("after exit: %+v, %v", ps, err)
	}
}

// startChrome runs a copy of the test binary from app as chrome.exe, until
// the returned function closes its stdin.
func startChrome(t *testing.T, app string) func() {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(app, "chrome.exe")
	if _, err := os.Stat(exe); err != nil {
		os.MkdirAll(app, 0o755)
		b, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(exe, b, 0o755)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "PITSTOP_WAIT=1")
	stdin, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stop := func() { stdin.Close(); cmd.Wait() }
	t.Cleanup(stop)
	return stop
}

func TestWait(t *testing.T) {
	app := filepath.Join(t.TempDir(), "App")
	stopA := startChrome(t, app)
	stopB := startChrome(t, app)
	done := make(chan error, 1)
	go func() { done <- Wait(context.Background(), app) }()
	stopA()
	select {
	case err := <-done:
		t.Fatalf("Wait returned with one process left: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	stopB()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait didn't return once Chrome exited")
	}
}

func TestWaitStopsWithItsContext(t *testing.T) {
	app := filepath.Join(t.TempDir(), "App")
	startChrome(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := Wait(ctx, app); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait = %v", err)
	}
}

func TestRoots(t *testing.T) {
	ps := []Process{{PID: 1, Parent: 900}, {PID: 2, Parent: 1}, {PID: 3, Parent: 2}, {PID: 4, Parent: 901}}
	if got := fmt.Sprint(roots(ps)); got != "[{1 900 } {4 901 }]" {
		t.Errorf("roots = %s", got)
	}
}
