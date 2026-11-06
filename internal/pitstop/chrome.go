package pitstop

import (
	"context"
	"errors"
	"time"

	"github.com/Bush2021/pitstop/internal/chromeproc"
)

// closeWait is how long Chrome gets to quit. Beyond it, a page is likely
// asking about something unsaved, which the user has to answer.
const closeWait = 20 * time.Second

func (e *Engine) ChromeRunning() bool { return errors.Is(e.closed(), ErrChromeRunning) }

func (e *Engine) StartChrome() error { return e.Hooks.Start(e.path(app, "chrome.exe")) }

// CloseChrome asks Chrome to quit and waits for it, or returns a
// *RunningError.
func (e *Engine) CloseChrome(ctx context.Context) error {
	e.report(Closing)(0, 0)
	if err := e.Hooks.Close(e.path(app)); errors.Is(err, chromeproc.ErrElevated) {
		// Waiting would take the whole closeWait for nothing.
		err := e.closed()
		if r, ok := errors.AsType[*RunningError](err); ok {
			r.Elevated = true
		}
		return err
	} else if err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(ctx, closeWait)
	defer cancel()
	err := e.Hooks.Wait(wait, e.path(app))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return e.closed()
}

func (e *Engine) RestartChrome(ctx context.Context) error {
	if err := e.CloseChrome(ctx); err != nil {
		return err
	}
	return e.StartChrome()
}
