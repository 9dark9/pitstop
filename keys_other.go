//go:build !windows

package main

import "github.com/egoist/mygo"

func keepKeys(*mygo.Window, func() bool) {}
