package pitstop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bush2021/chrome_update/omaha"
	"github.com/Bush2021/pitstop/internal/ini"
	"github.com/Bush2021/pitstop/internal/install"
	"github.com/Bush2021/pitstop/internal/pitstop/pitstoptest"
)

func discardReplaced(t *testing.T, engine *Engine) {
	t.Helper()
	if err := engine.UpdateState(func(s *install.State) { s.DiscardReplaced = true }); err != nil {
		t.Fatal(err)
	}
}

func updateTo(t *testing.T, engine *Engine, target string) {
	t.Helper()
	u, err := engine.checkChrome(context.Background(), omaha.Request{Channel: omaha.Stable, Arch: "x64", Target: target})
	if err != nil || u == nil {
		t.Fatalf("checkChrome = %+v, %v", u, err)
	}
	if _, err := engine.UpdateChrome(context.Background(), u); err != nil {
		t.Fatal(err)
	}
}

func moveDataOut(t *testing.T, engine *Engine) {
	t.Helper()
	path := engine.path(app, "chrome++.ini")
	f, err := ini.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("general", "data_dir", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
}

func wantNothingKept(t *testing.T, engine *Engine) {
	t.Helper()
	if got := entries(t, engine.path(rollback)); len(got) > 0 {
		t.Errorf("rollback holds %v", got)
	}
	if got := entries(t, engine.path(backup)); len(got) > 0 {
		t.Errorf("backup holds %v", got)
	}
	if exists(engine.path(journal)) {
		t.Error("a journal is left")
	}
}

func TestDeleteKeptChromeTakesTheProfileCopy(t *testing.T) {
	_, engine, u := milestoneUpdate(t)
	if _, err := engine.UpdateChrome(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	k := engine.KeptSizes()
	if k.Chrome == 0 || k.Profile == 0 {
		t.Errorf("KeptSizes = %+v", k)
	}
	closeChrome := running(engine)
	defer closeChrome()
	if err := engine.DeleteChromeRollback(); err != nil {
		t.Fatal(err)
	}
	wantNothingKept(t, engine)
	if k := engine.KeptSizes(); k != (KeptSizes{}) {
		t.Errorf("KeptSizes after deleting = %+v", k)
	}
	in, _ := engine.Open()
	if e := in.State.History[0]; e.Kind != install.KeptDeleted || e.To != "Chrome 155.0.8059.85" {
		t.Errorf("history starts with %+v", e)
	}
	if err := engine.DeleteChromeRollback(); !errors.Is(err, ErrNoRollback) {
		t.Errorf("deleting again = %v", err)
	}
}

func TestDeleteKeptChromePlus(t *testing.T) {
	s := pitstoptest.NewServers(t, "156.0.8078.74")
	engine := newEngine(t, s)
	in := setup(t, engine, "")
	pitstoptest.WriteChromePlus(t, filepath.Join(in.App(), "version.dll"), "1.18.2")
	in, _ = engine.Open()
	u, err := engine.CheckChromePlus(context.Background(), in)
	if err != nil || u == nil {
		t.Fatalf("CheckChromePlus = %+v, %v", u, err)
	}
	if err := engine.UpdateChromePlus(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if engine.KeptSizes().Plus == 0 {
		t.Error("the kept Chrome++ has no size")
	}
	if err := engine.DeleteChromePlusRollback(); err != nil {
		t.Fatal(err)
	}
	wantNothingKept(t, engine)
}

func TestDiscardReplacedKeepsNoChrome(t *testing.T) {
	s := pitstoptest.NewServers(t, "156.0.8078.48", "156.0.8078.74")
	engine := newEngine(t, s)
	setup(t, engine, "156.0.8078.48")
	discardReplaced(t, engine)
	updateTo(t, engine, "")
	wantNothingKept(t, engine)
	if in, _ := engine.Open(); in.Chrome != "156.0.8078.74" {
		t.Errorf("updated to %s", in.Chrome)
	}
}

func TestDiscardReplacedSkipsTheProfileCopy(t *testing.T) {
	_, engine, u := milestoneUpdate(t)
	discardReplaced(t, engine)
	var phases []Phase
	engine.Progress = func(st Step) { phases = append(phases, st.Phase) }
	if _, err := engine.UpdateChrome(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	for _, p := range phases {
		if p == BackingUp {
			t.Fatal("copied the profile")
		}
	}
	wantNothingKept(t, engine)
}

func TestDiscardReplacedClearsWhatIsKept(t *testing.T) {
	s := pitstoptest.NewServers(t, "155.0.8059.85", "156.0.8078.48", "156.0.8078.74")
	engine := newEngine(t, s)
	setup(t, engine, "155.")
	writeFiles(t, engine.path(data), map[string]string{"Local State": "155"})
	updateTo(t, engine, "156.0.8078.48")
	if !engine.ProfileKept("155.0.8059.85") {
		t.Fatal("no copy of the profile")
	}
	discardReplaced(t, engine)
	if _, ok := engine.ChromeRollback(); !ok {
		t.Fatal("turning it off cleared the kept build")
	}
	updateTo(t, engine, "")
	wantNothingKept(t, engine)
}

func TestDiscardReplacedKeepsNoChromePlus(t *testing.T) {
	s := pitstoptest.NewServers(t, "156.0.8078.74")
	engine := newEngine(t, s)
	in := setup(t, engine, "")
	pitstoptest.WriteChromePlus(t, filepath.Join(in.App(), "version.dll"), "1.18.2")
	discardReplaced(t, engine)
	in, _ = engine.Open()
	u, err := engine.CheckChromePlus(context.Background(), in)
	if err != nil || u == nil {
		t.Fatalf("CheckChromePlus = %+v, %v", u, err)
	}
	if err := engine.UpdateChromePlus(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	wantNothingKept(t, engine)
}

func TestDiscardReplacedAppliesToAnInUseUpdate(t *testing.T) {
	_, engine, _ := stageWhileRunning(t)
	discardReplaced(t, engine)
	swapStaged(t, engine.path(app), -1)
	in, err := engine.Open()
	if err != nil || in.Chrome != "156.0.8078.74" {
		t.Fatalf("after Chrome's restart: %+v, %v", in, err)
	}
	wantNothingKept(t, engine)
}

func TestSwitchKeepsTheCurrentVersionWithDiscardReplaced(t *testing.T) {
	s := pitstoptest.NewServers(t, "156.0.8078.48", "156.0.8078.74")
	engine := newEngine(t, s)
	setup(t, engine, "156.0.8078.48")
	updateTo(t, engine, "")
	discardReplaced(t, engine)
	if err := engine.RollbackChrome(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if v, ok := engine.ChromeRollback(); !ok || v != "156.0.8078.74" {
		t.Errorf("ChromeRollback = %q, %v", v, ok)
	}
}

func TestFailedUpdateLeavesChromeWithDiscardReplaced(t *testing.T) {
	s := pitstoptest.NewServers(t, "156.0.8078.48", "156.0.8078.74")
	engine := newEngine(t, s)
	in := setup(t, engine, "156.0.8078.48")
	discardReplaced(t, engine)
	u, _ := engine.CheckChrome(context.Background(), in)
	// A file where the folder has to go stops the swap at its first move.
	if err := os.WriteFile(engine.path(rollback), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.UpdateChrome(context.Background(), u); err == nil {
		t.Fatal("the update went through")
	}
	if in, err := engine.Open(); err != nil || in.Chrome != "156.0.8078.48" {
		t.Errorf("after the failure: %+v, %v", in, err)
	}
	if !exists(engine.path(app, "chrome.exe")) || !exists(engine.path(app, "156.0.8078.48", "chrome.dll")) {
		t.Errorf("App holds %v", entries(t, engine.path(app)))
	}
}

func TestReplacingTheKeptBuildDropsItsProfileCopy(t *testing.T) {
	s := pitstoptest.NewServers(t, "155.0.8059.85", "156.0.8078.48", "156.0.8078.74")
	engine := newEngine(t, s)
	setup(t, engine, "155.")
	writeFiles(t, engine.path(data), map[string]string{"Local State": "155"})
	updateTo(t, engine, "156.0.8078.48")
	updateTo(t, engine, "")
	if got := entries(t, engine.path(backup)); len(got) > 0 {
		t.Errorf("backup holds %v", got)
	}
	if v, ok := engine.ChromeRollback(); !ok || v != "156.0.8078.48" {
		t.Errorf("ChromeRollback = %q, %v", v, ok)
	}
}

func TestProfileElsewhereIsNotCopied(t *testing.T) {
	_, engine, u := milestoneUpdate(t)
	moveDataOut(t, engine)
	if _, err := engine.UpdateChrome(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, engine.path(backup)); len(got) > 0 {
		t.Errorf("backup holds %v", got)
	}
	if engine.ProfileKept("155.0.8059.85") {
		t.Error("ProfileKept = true")
	}
}

func TestCopyIsNotPutBackOnceTheProfileMoved(t *testing.T) {
	_, engine, u := milestoneUpdate(t)
	if _, err := engine.UpdateChrome(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	moveDataOut(t, engine)
	writeFiles(t, engine.path(data), map[string]string{"Default/History": "left behind"})
	if engine.ProfileKept("155.0.8059.85") {
		t.Fatal("ProfileKept = true")
	}
	if err := engine.RollbackChrome(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	in, _ := engine.Open()
	if got := files(t, engine.path(data)); in.Chrome != "155.0.8059.85" || got["Default/History"] != "left behind" {
		t.Fatalf("after the switch: %s, Data holds %v", in.Chrome, got)
	}
	if got := fmt.Sprint(entries(t, engine.path(backup))); got != "[]" {
		t.Errorf("backup holds %s", got)
	}
}
