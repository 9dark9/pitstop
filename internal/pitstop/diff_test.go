package pitstop

import (
	"context"
	"fmt"
	"testing"

	"github.com/Bush2021/pitstop/internal/install"
	"github.com/Bush2021/pitstop/internal/pitstop/pitstoptest"
)

const (
	v48 = "156.0.8078.48"
	v74 = "156.0.8078.74"
	v80 = "156.0.8078.80"
)

func update(t *testing.T, engine *Engine) *ChromeUpdate {
	t.Helper()
	in, err := engine.Open()
	if err != nil {
		t.Fatal(err)
	}
	u, err := engine.CheckChrome(context.Background(), in)
	if err != nil || u == nil {
		t.Fatalf("CheckChrome = %+v, %v", u, err)
	}
	if _, err := engine.UpdateChrome(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if in, _ = engine.Open(); in.Chrome != u.Version {
		t.Fatalf("installed %s, want %s", in.Chrome, u.Version)
	}
	return u
}

func setDiffUpdates(t *testing.T, engine *Engine, on bool) {
	t.Helper()
	if err := engine.UpdateState(func(s *install.State) { s.DiffUpdates = on }); err != nil {
		t.Fatal(err)
	}
}

// diffEngine has 74 installed with its package kept, and offers 80.
func diffEngine(t *testing.T) (*pitstoptest.Servers, *Engine) {
	s := pitstoptest.NewServers(t, v48, v74)
	engine := newEngine(t, s)
	setup(t, engine, v48)
	setDiffUpdates(t, engine, true)
	if u := update(t, engine); u.Fallback != nil || s.Diffs.Load() != 0 {
		t.Fatalf("an update without a kept package was differential: %+v", u)
	}
	if base, ok := engine.base(); !ok || base != pitstoptest.Sum(pitstoptest.Package(v74)) {
		t.Fatalf("kept %q", base)
	}
	s.Versions = append(s.Versions, v80)
	return s, engine
}

func TestDifferentialChromeUpdate(t *testing.T) {
	s, engine := diffEngine(t)
	var phases []Phase
	engine.Progress = func(st Step) {
		if len(phases) == 0 || phases[len(phases)-1] != st.Phase {
			phases = append(phases, st.Phase)
		}
	}
	gets := s.Gets.Load()
	u := update(t, engine)
	if u.Pipeline.Kind() != "zucc" || u.Fallback == nil || u.Fallback.Kind() != "xz" {
		t.Errorf("pipeline %s, fallback %+v", u.Pipeline.ID, u.Fallback)
	}
	if fmt.Sprint(s.Cached) != fmt.Sprint([]string{pitstoptest.Sum(pitstoptest.Package(v74))}) {
		t.Errorf("reported %v as cached", s.Cached)
	}
	if s.Diffs.Load() != 1 || s.Gets.Load() != gets+1 {
		t.Errorf("%d differential downloads of %d", s.Diffs.Load(), s.Gets.Load()-gets)
	}
	if want := "[download decode diff unpack verify apply]"; fmt.Sprint(phases) != want {
		t.Errorf("phases %v, want %s", phases, want)
	}
	if base, _ := engine.base(); base != pitstoptest.Sum(pitstoptest.Package(v80)) {
		t.Errorf("kept %q after the differential update", base)
	}
	if got := fmt.Sprint(entries(t, engine.path(chromeCache))); got != "[base]" {
		t.Errorf("cache holds %s", got)
	}
}

func TestDifferentialChromeUpdateFallsBack(t *testing.T) {
	s, engine := diffEngine(t)
	s.BadDiff = true
	gets := s.Gets.Load()
	update(t, engine)
	if s.Diffs.Load() != 1 || s.Gets.Load() != gets+2 {
		t.Errorf("%d differential downloads of %d", s.Diffs.Load(), s.Gets.Load()-gets)
	}
	if base, _ := engine.base(); base != pitstoptest.Sum(pitstoptest.Package(v80)) {
		t.Errorf("kept %q after the full download", base)
	}
	if got := fmt.Sprint(entries(t, engine.path(chromeCache))); got != "[base]" {
		t.Errorf("cache holds %s", got)
	}
}

func TestTurningDiffUpdatesOffDeletesThePackage(t *testing.T) {
	s, engine := diffEngine(t)
	setDiffUpdates(t, engine, false)
	if exists(engine.path(baseCache)) {
		t.Error("the package is kept")
	}
	if u := update(t, engine); u.Fallback != nil || len(s.Cached) != 0 || s.Diffs.Load() != 0 {
		t.Errorf("was differential with the setting off: %s, cached %v", u.Pipeline.ID, s.Cached)
	}
	if exists(engine.path(baseCache)) || len(entries(t, engine.path(chromeCache))) != 0 {
		t.Errorf("cache holds %v", entries(t, engine.path(chromeCache)))
	}
}
