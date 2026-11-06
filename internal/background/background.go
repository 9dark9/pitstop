// Package background is Pitstop's background mode. Chrome++ starts it with
// Chrome when the update policy isn't Off. While Chrome runs, it checks for
// updates and applies the policy. Once Chrome exits, it installs what was
// approved, and quits.
package background

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/Bush2021/pitstop/internal/install"
	"github.com/Bush2021/pitstop/internal/pitstop"
)

// retry is how soon a check that got no answer runs again.
const retry = time.Hour

// Offer is an update the background mode asks about.
type Offer struct {
	Name, Version string
	chrome        *pitstop.ChromeUpdate
}

// UI is how the background mode talks to the user.
type UI interface {
	// Ask offers an update, which the user may approve (Runner.Approve) or
	// skip (Runner.Skip).
	Ask(o Offer)
	// Installed tells what went in once Chrome exited.
	Installed(what string)
	Failed(err error)
}

type Runner struct {
	Engine     *pitstop.Engine
	UI         UI
	UpdateSelf func(context.Context) error
	// Conditions reports the time and what the computer allows now.
	Conditions func() pitstop.Conditions
	// Wait returns once no process runs from dir: chromeproc.Wait.
	Wait func(ctx context.Context, dir string) error
	// Linger is how long it stays after Chrome exits once it told the
	// user something, as its notification goes when it quits.
	Linger time.Duration

	// asked lists the versions asked about while this Chrome runs.
	asked map[string]bool
}

// Run returns once Chrome exits, or the policy is Off.
func (r *Runner) Run(ctx context.Context) error {
	for {
		in, err := r.Engine.Open()
		if err != nil {
			return err
		}
		if in.State.Policy == install.Off {
			return nil
		}
		now := r.Conditions().Now
		next := in.State.Checked.Add(in.State.CheckEvery())
		if !now.Before(next) || in.State.Checked.After(now) {
			next = now.Add(retry)
			if r.check(ctx, in) {
				next = now.Add(in.State.CheckEvery())
			}
		}
		wctx, cancel := context.WithTimeout(ctx, next.Sub(r.Conditions().Now))
		err = r.Wait(wctx, in.App())
		cancel()
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, context.DeadlineExceeded):
			continue
		case err != nil:
			return err
		}
		if r.afterExit(ctx) && !sleep(ctx, r.Linger) {
			return ctx.Err()
		}
		// Chrome may have started again meanwhile, and its Pitstop quit
		// as this one runs.
		if ps, err := r.Engine.Hooks.Running(in.App()); err != nil || len(ps) == 0 {
			return err
		}
	}
}

// check looks for updates, applies the policy and reports whether both
// sources answered. It asks one question at a time.
func (r *Runner) check(ctx context.Context, in *install.Install) bool {
	c := r.Conditions()
	if in.State.Policy == install.Automatic && !c.Metered {
		if err := r.UpdateSelf(ctx); err != nil {
			log.Printf("update Pitstop: %v", err)
		}
	}
	u, chromeErr := r.Engine.CheckChrome(ctx, in)
	p, plusErr := r.Engine.CheckChromePlus(ctx, in)
	if err := errors.Join(chromeErr, plusErr); err != nil {
		log.Printf("check: %v", err)
	} else if err := r.setState(ctx, func(s *install.State) { s.Checked = c.Now }); err != nil {
		log.Printf("recording the check: %v", err)
	}
	asked := false
	if u != nil {
		a := pitstop.Decide(in.State, in.State.Approved, u.Version, c.Metered)
		log.Printf("check: Chrome %s: %s", u.Version, a)
		switch a {
		case pitstop.Ask:
			asked = r.ask(Offer{Name: "Chrome", Version: u.Version, chrome: u})
		case pitstop.Get:
			if err := r.get(ctx, u); err != nil {
				log.Printf("install Chrome %s: %v", u.Version, err)
				r.fail(ctx, "Chrome "+u.Version, err)
			}
		}
	}
	if p != nil {
		v := p.Release.Tag
		a := pitstop.Decide(in.State, in.State.ApprovedPlus, v, c.Metered)
		log.Printf("check: Chrome++ Next %s: %s", v, a)
		switch {
		case a == pitstop.Ask && !asked:
			r.ask(Offer{Name: "Chrome++ Next", Version: v})
		case a == pitstop.Get:
			if err := r.setState(ctx, func(s *install.State) { s.ApprovedPlus = v }); err != nil {
				log.Printf("approve Chrome++ Next %s: %v", v, err)
			}
		}
	}
	return chromeErr == nil && plusErr == nil
}

func (r *Runner) ask(o Offer) bool {
	if r.asked[o.Version] {
		return false
	}
	if r.asked == nil {
		r.asked = map[string]bool{}
	}
	r.asked[o.Version] = true
	r.UI.Ask(o)
	return true
}

// get installs u as far as Chrome allows, and records what has to wait for
// Chrome to exit.
func (r *Runner) get(ctx context.Context, u *pitstop.ChromeUpdate) error {
	var outcome pitstop.Outcome
	err := r.whenFree(ctx, func() (err error) {
		outcome, err = r.Engine.UpdateChrome(ctx, u)
		return err
	})
	switch {
	case errors.Is(err, pitstop.ErrChromeRunning):
		return r.setState(ctx, func(s *install.State) { s.Approved = u.Version })
	case err == nil && outcome == pitstop.Installed:
		r.UI.Installed("Chrome " + u.Version)
	}
	return err
}

// afterExit installs what was approved and reports whether it told the
// user something.
func (r *Runner) afterExit(ctx context.Context) bool {
	var done []string
	put := func(name string, fn func(context.Context) (string, error)) bool {
		var v string
		err := r.whenFree(ctx, func() (err error) {
			v, err = fn(ctx)
			return err
		})
		switch {
		case errors.Is(err, pitstop.ErrChromeRunning):
		case err != nil:
			log.Printf("install the approved %s: %v", name, err)
			r.fail(ctx, name, err)
			return true
		case v != "":
			log.Printf("installed %s %s", name, v)
			done = append(done, name+" "+v)
		}
		return false
	}
	failed := put("Chrome", r.Engine.InstallApproved)
	failed = put("Chrome++ Next", r.Engine.InstallApprovedPlus) || failed
	if len(done) > 0 {
		r.UI.Installed(strings.Join(done, ", "))
	}
	return failed || len(done) > 0
}

func (r *Runner) fail(ctx context.Context, what string, err error) {
	r.UI.Failed(err)
	r.remember(ctx, what, err)
}

// remember keeps a failure for the window to show.
func (r *Runner) remember(ctx context.Context, what string, err error) {
	f := &install.Failure{Time: r.Conditions().Now, What: what, Error: err.Error()}
	if err := r.setState(ctx, func(s *install.State) { s.Failure = f }); err != nil {
		log.Printf("recording the failure: %v", err)
	}
}

// Approve has the offered update installed: Chrome's download starts now,
// and what can't go in while Chrome runs waits for it to exit.
func (r *Runner) Approve(ctx context.Context, o Offer) error {
	if o.chrome == nil {
		return r.setState(ctx, func(s *install.State) { s.ApprovedPlus = o.Version })
	}
	if err := r.setState(ctx, func(s *install.State) { s.Approved = o.Version }); err != nil {
		return err
	}
	err := r.get(ctx, o.chrome)
	if err != nil {
		r.remember(ctx, "Chrome "+o.Version, err)
	}
	return err
}

func (r *Runner) Skip(ctx context.Context, o Offer) error {
	return r.setState(ctx, func(s *install.State) { s.Skip(o.Version) })
}

// setState changes the state once the install is free.
func (r *Runner) setState(ctx context.Context, change func(*install.State)) error {
	return r.whenFree(ctx, func() error { return r.Engine.UpdateState(change) })
}

// whenFree runs fn until the install's lock doesn't stop it: the window, or
// an approved download, may hold it.
func (r *Runner) whenFree(ctx context.Context, fn func() error) error {
	for {
		err := fn()
		if !errors.Is(err, pitstop.ErrBusy) {
			return err
		}
		if !sleep(ctx, time.Second) {
			return ctx.Err()
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
