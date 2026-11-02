//go:build !windows

package apply

func transient(error) bool { return false }
