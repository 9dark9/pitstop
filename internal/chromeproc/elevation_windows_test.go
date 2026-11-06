package chromeproc

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

const requireAdmin = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
 <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges>
  <requestedExecutionLevel level="requireAdministrator" uiAccess="false"/>
 </requestedPrivileges></security></trustInfo>
</assembly>`

// A chrome.exe that must run as administrator, as one set to in its
// compatibility settings, starts through the shell instead of failing.
func TestStartAsksToElevate(t *testing.T) {
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("CreateProcess elevates nothing from an elevated process")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "chrome.exe")
	if err := os.WriteFile(exe, b, 0o755); err != nil {
		t.Fatal(err)
	}
	addManifest(t, exe, requireAdmin)

	var opened string
	defer func(f func(string) error) { shellOpen = f }(shellOpen)
	shellOpen = func(exe string) error { opened = exe; return nil }
	if err := Start(exe); err != nil || opened != exe {
		t.Errorf("Start = %v, opened %q", err, opened)
	}
}

func addManifest(t *testing.T, exe, manifest string) {
	t.Helper()
	k := windows.NewLazySystemDLL("kernel32.dll")
	h, _, err := k.NewProc("BeginUpdateResourceW").Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(exe))), 0)
	if h == 0 {
		t.Fatal(err)
	}
	data := []byte(manifest)
	if ok, _, err := k.NewProc("UpdateResourceW").Call(h, uintptr(windows.RT_MANIFEST), uintptr(windows.CREATEPROCESS_MANIFEST_RESOURCE_ID),
		0x409, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data))); ok == 0 {
		t.Fatal(err)
	}
	if ok, _, err := k.NewProc("EndUpdateResourceW").Call(h, 0); ok == 0 {
		t.Fatal(err)
	}
}
