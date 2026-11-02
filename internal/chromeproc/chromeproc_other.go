//go:build !windows

package chromeproc

import "context"

// Running finds nothing outside Windows, where Chrome's files are not
// locked while it runs.
func Running(string) ([]Process, error) { return nil, nil }

func Wait(context.Context, string) error { return nil }

func Close(string) error { return nil }
