package main

import (
	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

const (
	wmSysKeyDown = 0x104
	wmSysKeyUp   = 0x105
	wmSysCommand = 0x112
	scClose      = 0xF060
	scKeyMenu    = 0xF100
)

var (
	comctl32          = windows.NewLazySystemDLL("comctl32.dll")
	setWindowSubclass = comctl32.NewProc("SetWindowSubclass")
	defSubclassProc   = comctl32.NewProc("DefSubclassProc")
)

// keepKeys stops Windows from acting on the keys that a hotkey field takes.
// mygo hands them to the view and to DefWindowProc both, which would close
// the window on Alt+F4. An Alt tap and F10 look for a menu bar the window
// doesn't have, and leave the view without keys until the next one, so they
// never get that far.
func keepKeys(win *mygo.Window, recording func() bool) {
	taken := false
	proc := windows.NewCallback(func(hwnd, msg, wp, lp, id, data uintptr) uintptr {
		switch msg {
		case wmSysKeyDown:
			taken = recording() // before the key ends the recording
		case wmSysKeyUp:
			taken = false
		case wmSysCommand:
			switch wp &^ 0xF {
			case scKeyMenu:
				if lp == 0 || taken {
					return 0
				}
			case scClose:
				if lp == 0 && taken {
					return 0
				}
			}
		}
		r, _, _ := defSubclassProc.Call(hwnd, msg, wp, lp)
		return r
	})
	// The keys go to the child window mygo draws in, and what DefWindowProc
	// makes of them to the window.
	hwnd := win.NativeHandle()
	setWindowSubclass.Call(hwnd, proc, 1, 0)
	windows.EnumChildWindows(windows.HWND(hwnd), windows.NewCallback(func(child, _ uintptr) uintptr {
		setWindowSubclass.Call(child, proc, 1, 0)
		return 1
	}), nil)
}
