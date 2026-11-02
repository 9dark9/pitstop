package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/Bush2021/pitstop/internal/background"
	"github.com/Bush2021/pitstop/internal/install"
	"github.com/Bush2021/pitstop/internal/pitstop"
)

func TestFlyoutAnswers(t *testing.T) {
	for _, tc := range []struct {
		button   string
		approved string
		skipped  int
	}{
		{"Update when I close Chrome", "1.19.1", 0},
		{"Skip this version", "", 1},
	} {
		engine, err := pitstop.New(t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		n := &notifier{root: engine.Root, r: &background.Runner{Engine: engine}, asked: &background.Offer{Name: "Chrome++ Next", Version: "1.19.1"}}
		tt := ui.NewTester(n.view, 360, 190)
		for _, s := range []string{"Chrome++ Next 1.19.1 is available", "Skip this version"} {
			if !tt.HasText(s) {
				t.Fatalf("the flyout shows %q", tt.Texts())
			}
		}
		if dir := os.Getenv("PITSTOP_SHOTS"); dir != "" && tc.skipped == 0 {
			f, _ := os.Create(filepath.Join(dir, "flyout.png"))
			png.Encode(f, tt.Image())
			f.Close()
		}
		if err := tt.Click(tc.button); err != nil {
			t.Fatal(err)
		}
		var s install.State
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if s, _ = install.LoadState(engine.Root); s.ApprovedPlus != "" || len(s.Skipped) > 0 {
				break
			}
		}
		if s.ApprovedPlus != tc.approved || len(s.Skipped) != tc.skipped || n.asked != nil {
			t.Errorf("%s: approved %q, skipped %v, still asking: %v", tc.button, s.ApprovedPlus, s.Skipped, n.asked != nil)
		}
	}
}

func TestFlyoutOffersARestartAfterAnInstall(t *testing.T) {
	n := &notifier{status: "Chrome 157.0.8100.10, Chrome++ Next 1.19.1 is ready", restart: true}
	tt := ui.NewTester(n.view, 360, 160)
	for _, s := range []string{"Restart Chrome", "Open Pitstop"} {
		if !tt.HasText(s) {
			t.Fatalf("the flyout shows %q", tt.Texts())
		}
	}
	if dir := os.Getenv("PITSTOP_SHOTS"); dir != "" {
		f, _ := os.Create(filepath.Join(dir, "flyout-installed.png"))
		png.Encode(f, tt.Image())
		f.Close()
	}
}

// The flyout is as small as openFlyout makes it, and a long error, as a
// path makes it, keeps the button in view.
func TestFlyoutFitsALongError(t *testing.T) {
	n := &notifier{status: `an interrupted change (chromeUpdated) could not be finished: rename D:\Chrome\123\.pitstop\staging\chrome-158.0.8127.2\App\chrome_proxy.exe D:\Chrome\123\App\chrome_proxy.exe: The process cannot access the file because it is being used by another process.`}
	tt := ui.NewTester(n.view, 360, 160)
	if dir := os.Getenv("PITSTOP_SHOTS"); dir != "" {
		f, _ := os.Create(filepath.Join(dir, "flyout-error.png"))
		png.Encode(f, tt.Image())
		f.Close()
	}
	r, ok := tt.Find("Open Pitstop")
	if !ok || r.Y+r.H > 160-16 {
		t.Fatalf("Open Pitstop at %+v in a flyout 160 high", r)
	}
}
