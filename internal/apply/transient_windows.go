package apply

import (
	"errors"

	"golang.org/x/sys/windows"
)

func transient(err error) bool {
	errno, ok := errors.AsType[windows.Errno](err)
	if !ok {
		return false
	}
	switch errno {
	case windows.ERROR_ACCESS_DENIED, windows.ERROR_SHARING_VIOLATION, windows.ERROR_LOCK_VIOLATION:
		return true
	}
	return false
}
