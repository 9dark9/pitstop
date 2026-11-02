package pitstop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/netproxy"

	"github.com/Bush2021/chrome_update/omaha"
	"github.com/Bush2021/pitstop/internal/chromeproc"
	"github.com/Bush2021/pitstop/internal/install"
	"github.com/Bush2021/pitstop/internal/verify"
)

const liveChild = "PITSTOP_LIVE_CHILD"

// TestLive installs Chrome and Chrome++ from Google's and GitHub's servers
// into a scratch folder, then updates, rolls back while Chrome runs, and
// resumes a download whose process was killed. It downloads Chrome three
// times.
func TestLive(t *testing.T) {
	if os.Getenv("PITSTOP_LIVE") == "" {
		t.Skip("set PITSTOP_LIVE=1 to install Chrome from Google's servers")
	}
	ctx := context.Background()
	if root := os.Getenv(liveChild); root != "" {
		// The process that the last step kills while it downloads.
		engine := liveEngine(t, root)
		u, err := engine.CheckChrome(ctx, reopen(t, engine))
		if err != nil || u == nil {
			t.Fatalf("check: %v, %v", u, err)
		}
		if _, err := engine.UpdateChrome(ctx, u); err != nil {
			t.Fatal(err)
		}
		return
	}

	root := filepath.Join(t.TempDir(), "Chrome")
	engine := liveEngine(t, root)
	appDir := engine.path(app)
	t.Cleanup(func() { stopChrome(t, appDir) })

	newest, err := engine.checkChrome(ctx, omaha.Request{Channel: omaha.Stable, Arch: "x64"})
	if err != nil || newest == nil {
		t.Fatalf("check the newest stable Chrome: %v, %v", newest, err)
	}
	// One milestone back, so that rolling back crosses a milestone.
	target := fmt.Sprintf("%d.", Milestone(newest.Version)-1)
	if err := engine.Setup(ctx, SetupOptions{Channel: omaha.Stable, Arch: "x64", Target: target}); err != nil {
		t.Fatalf("set up Chrome %s: %v", target, err)
	}
	in := reopen(t, engine)
	older := in.Chrome
	if in.ChromePlus == "" || in.Injection != install.VersionDLL {
		t.Fatalf("Chrome++ %q injected as %v; want a release as version.dll", in.ChromePlus, in.Injection)
	}
	signed(t, in)
	if u, err := engine.CheckChromePlus(ctx, in); err != nil || u != nil {
		t.Fatalf("check Chrome++ right after setup: %v, %v", u, err)
	}

	u, err := engine.CheckChrome(ctx, in)
	if err != nil || u == nil {
		t.Fatalf("check: %v, %v", u, err)
	}
	if _, err := engine.UpdateChrome(ctx, u); err != nil {
		t.Fatalf("update to %s: %v", u.Version, err)
	}
	if in = reopen(t, engine); in.Chrome != u.Version {
		t.Fatalf("Chrome is %s after the update to %s", in.Chrome, u.Version)
	}
	signed(t, in)

	if err := engine.RollbackChrome(ctx, false); !errors.Is(err, ErrMilestone) {
		t.Fatalf("roll back to %s: %v, want ErrMilestone", older, err)
	}
	if runtime.GOOS == "windows" {
		chrome := exec.Command(filepath.Join(appDir, "chrome.exe"), "--headless=new", "--no-first-run", "--disable-gpu", "about:blank")
		if err := chrome.Start(); err != nil {
			t.Fatal(err)
		}
		if !waitFor(func() bool { ps, err := chromeproc.Running(appDir); return err == nil && len(ps) > 0 }) {
			t.Fatal("Chrome did not start")
		}
		err := engine.RollbackChrome(ctx, true)
		if !errors.Is(err, ErrChromeRunning) {
			t.Fatalf("roll back while Chrome runs: %v, want ErrChromeRunning", err)
		}
		t.Logf("refused while Chrome runs: %v", err)
		stopChrome(t, appDir)
		chrome.Wait()
	}
	if err := engine.RollbackChrome(ctx, true); err != nil {
		t.Fatalf("roll back to %s: %v", older, err)
	}
	if in = reopen(t, engine); in.Chrome != older {
		t.Fatalf("Chrome is %s after the rollback to %s", in.Chrome, older)
	}

	child := exec.Command(os.Args[0], "-test.run=^TestLive$")
	child.Env = append(os.Environ(), liveChild+"="+root)
	var out bytes.Buffer
	child.Stdout, child.Stderr = &out, &out
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	var part string
	for size := int64(0); size < 32<<20; {
		select {
		case err := <-exited:
			t.Fatalf("the update ended before it was killed (%v):\n%s", err, out.String())
		case <-time.After(100 * time.Millisecond):
		}
		matches, _ := filepath.Glob(engine.path(chromeCache, "*.part"))
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil {
				part, size = m, st.Size()
			}
		}
	}
	child.Process.Kill()
	<-exited
	st, err := os.Stat(part)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("killed the download at %d of %d MB", st.Size()>>20, u.DownloadSize()>>20)

	resumed := int64(-1)
	logStep := engine.Progress
	engine.Progress = func(s Step) {
		if s.Phase == Downloading && resumed < 0 {
			resumed = s.Done
		}
		logStep(s)
	}
	if _, err := engine.UpdateChrome(ctx, u); err != nil {
		t.Fatalf("update after the kill: %v", err)
	}
	if resumed < st.Size() {
		t.Errorf("the download started again at byte %d; %d were kept", resumed, st.Size())
	}
	if in = reopen(t, engine); in.Chrome != u.Version {
		t.Fatalf("Chrome is %s after the update to %s", in.Chrome, u.Version)
	}
	signed(t, in)

	for _, dir := range []string{app, meta, "Data"} {
		t.Logf("%-8s %6d MB", dir, size(engine.path(dir))>>20)
	}
}

func liveEngine(t *testing.T, root string) *Engine {
	t.Helper()
	engine, err := New(root, &http.Client{Transport: netproxy.Transport()})
	if err != nil {
		t.Fatal(err)
	}
	var phase Phase
	tenth := int64(-1)
	engine.Progress = func(s Step) {
		n := int64(-1)
		if s.Total > 0 {
			n = s.Done * 10 / s.Total
		}
		if s.Phase == phase && n == tenth {
			return
		}
		phase, tenth = s.Phase, n
		if n < 0 {
			t.Logf("%s", s.Phase)
		} else {
			t.Logf("%s %d%%", s.Phase, n*10)
		}
	}
	return engine
}

func reopen(t *testing.T, engine *Engine) *install.Install {
	t.Helper()
	in, err := engine.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Chrome %s with Chrome++ %s", in.Chrome, in.ChromePlus)
	return in
}

func signed(t *testing.T, in *install.Install) {
	t.Helper()
	dir := in.App()
	err := verify.SignedBy(verify.Google,
		filepath.Join(dir, "chrome.exe"),
		filepath.Join(dir, "chrome_proxy.exe"),
		filepath.Join(dir, in.Chrome, "chrome.dll"))
	if err != nil && !errors.Is(err, verify.ErrUnsupported) {
		t.Fatal(err)
	}
}

// stopChrome kills every process running from dir, again and again, since
// a dying browser may still start a helper.
func stopChrome(t *testing.T, dir string) {
	t.Helper()
	ok := waitFor(func() bool {
		ps, err := chromeproc.Running(dir)
		for _, p := range ps {
			if proc, err := os.FindProcess(int(p.PID)); err == nil {
				proc.Kill()
			}
		}
		return err == nil && len(ps) == 0
	})
	if !ok {
		t.Errorf("Chrome still runs from %s", dir)
	}
}

func waitFor(cond func() bool) bool {
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return false
}

func size(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
