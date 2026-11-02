package pitstop

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/Bush2021/pitstop/internal/ini"
	"github.com/Bush2021/pitstop/internal/install"
	"github.com/Bush2021/pitstop/internal/pitstop/pitstoptest"
)

// Chrome++ reads the launch line through GetPrivateProfileStringW, which
// drops the outer quotes of a value that starts and ends with one.
func TestWindowsReadsTheLaunchLine(t *testing.T) {
	s := pitstoptest.NewServers(t, "156.0.8078.48")
	engine := newEngine(t, s)
	in := setup(t, engine, "")
	for _, exe := range []string{filepath.Join(engine.Root, "pitstop.exe"), `C:\Tools\pitstop.exe`} {
		engine.Exe = exe
		if err := engine.SetPolicy(install.Ask); err != nil {
			t.Fatal(err)
		}
		want, _ := launchLine(t, in)
		if got := profileString(t, filepath.Join(in.App(), "chrome++.ini"), "general", launchOnStartup); got != want {
			t.Errorf("pitstop.exe at %s: Windows reads %s from %s", exe, got, want)
		}
	}
}

// Taking Pitstop's command out of a line must not leave a value that starts
// and ends with a quote, or Windows breaks the user's own command.
func TestWindowsKeepsTheQuotesOfAUserCommand(t *testing.T) {
	s := pitstoptest.NewServers(t, "156.0.8078.48")
	engine := newEngine(t, s)
	in := setup(t, engine, "")
	path := filepath.Join(in.App(), "chrome++.ini")
	const user = `"C:\Sync Tool\sync.exe" --dir "C:\My Files"`
	f, err := ini.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Set("general", launchOnStartup, user+";"+engine.backgroundCommand())
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := engine.SetPolicy(install.Off); err != nil {
		t.Fatal(err)
	}
	if got := profileString(t, path, "general", launchOnStartup); strings.TrimSuffix(got, ";") != user {
		t.Errorf("Windows reads %s", got)
	}
}

func profileString(t *testing.T, path, section, key string) string {
	t.Helper()
	var args [4]*uint16
	for i, s := range []string{section, key, "", path} {
		p, err := syscall.UTF16PtrFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		args[i] = p
	}
	buf := make([]uint16, 4096)
	n, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetPrivateProfileStringW").Call(
		uintptr(unsafe.Pointer(args[0])), uintptr(unsafe.Pointer(args[1])), uintptr(unsafe.Pointer(args[2])),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(args[3])))
	return syscall.UTF16ToString(buf[:n])
}
