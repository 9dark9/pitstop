package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

func configureUpdates() {
	version, _, _ := strings.Cut(mygo.App.Version(), "+")
	mygo.Updater.SetPrereleases(strings.Contains(version, "-"))
}

type selfUpdater struct {
	installed bool
}

func (s *selfUpdater) install(ctx context.Context) error {
	if s.installed {
		return nil
	}
	// The Chrome check waits for this, and mygo's downloads have no timeout.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	u, err := mygo.Updater.Check(ctx)
	if err != nil || u == nil {
		return err
	}
	if err := u.Install(ctx, nil); err != nil {
		return err
	}
	s.installed = true
	log.Printf("installed Pitstop %s; effective at next launch", u.Version)
	return nil
}
