package install

import (
	"debug/pe"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Bush2021/chrome_update/omaha"
	"github.com/Bush2021/chrome_update/pefile/pefiletest"
)

const chromePlusDLL = "../../testdata/chromeplus-1.18.3-x64.dll"

// fakeChrome lays out App with a chrome.exe of version 156.0.8078.74 that
// imports the given DLLs.
func fakeChrome(t *testing.T, imports ...string) string {
	t.Helper()
	root := t.TempDir()
	app := filepath.Join(root, "App")
	if err := os.MkdirAll(filepath.Join(app, "156.0.8078.74"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := pefiletest.Image{
		Machine:     pe.IMAGE_FILE_MACHINE_AMD64,
		FileVersion: [4]uint16{156, 0, 8078, 74},
		Strings:     map[string]string{"FileDescription": "Google Chrome"},
		Imports:     append(imports, "KERNEL32.dll"),
	}
	if err := exe.Write(filepath.Join(app, "chrome.exe")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(app, "156.0.8078.74", "chrome.dll"), []byte("MZ"), 0o644)
	return root
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpen(t *testing.T) {
	for _, tc := range []struct {
		name      string
		imports   []string
		dll       string // copied from the Chrome++ fixture into App
		plus      string
		injection Injection
	}{
		{"version.dll", nil, "version.dll", "1.18.3", VersionDLL},
		{"setdll", []string{"version-x64.dll"}, "version-x64.dll", "1.18.3", SetDLL},
		{"plain Chrome", nil, "", "", None},
		{"setdll without the DLL", []string{"version-x64.dll"}, "", "", None},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fakeChrome(t, tc.imports...)
			if tc.dll != "" {
				copyFile(t, chromePlusDLL, filepath.Join(root, "App", tc.dll))
			}
			in, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if in.Chrome != "156.0.8078.74" || in.Arch != "x64" || in.ChromePlus != tc.plus || in.Injection != tc.injection || in.ChromePlusDLL != tc.dll {
				t.Errorf("Open = %+v", in)
			}
		})
	}
}

func TestManagesUpdates(t *testing.T) {
	const name = "alpha-7b80a11"
	fake := func(strs map[string]string) func(path string) {
		return func(path string) {
			strs["FileDescription"], strs["ProductVersion"] = chromePlusDescription, name
			pefiletest.Image{Machine: pe.IMAGE_FILE_MACHINE_AMD64, Strings: strs}.Write(path)
		}
	}
	for _, tc := range []struct {
		name    string
		write   func(path string)
		imports []string
		dll     string
		want    bool
	}{
		{"release before the key", func(path string) { copyFile(t, chromePlusDLL, path) }, nil, "version.dll", false},
		{"alpha without the key", fake(map[string]string{}), nil, "version.dll", false},
		{"alpha with the key", fake(map[string]string{"Comments": ManagedUpdatesKey}), nil, "version.dll", true},
		{"setdll with the key", fake(map[string]string{"Comments": ManagedUpdatesKey}), []string{"version-x64.dll"}, "version-x64.dll", true},
	} {
		root := fakeChrome(t, tc.imports...)
		tc.write(filepath.Join(root, "App", tc.dll))
		in, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		if in.ManagesUpdates != tc.want {
			t.Errorf("%s: ManagesUpdates = %v", tc.name, in.ManagesUpdates)
		}
	}
}

func TestOpenIgnoresTheSystemVersionDLL(t *testing.T) {
	root := fakeChrome(t)
	dll := pefiletest.Image{
		Machine:     pe.IMAGE_FILE_MACHINE_AMD64,
		FileVersion: [4]uint16{10, 0, 26100, 1},
		Strings:     map[string]string{"FileDescription": "Version Checking and File Installation Libraries"},
	}
	dll.Write(filepath.Join(root, "App", "version.dll"))
	in, err := Open(root)
	if err != nil || in.Injection != None || in.ChromePlus != "" {
		t.Fatalf("Open = %+v, %v", in, err)
	}
}

func TestOpenRejectsMissingFiles(t *testing.T) {
	if _, err := Open(t.TempDir()); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("empty folder: %v", err)
	}
	root := fakeChrome(t)
	os.Remove(filepath.Join(root, "App", "156.0.8078.74", "chrome.dll"))
	if _, err := Open(root); err == nil || errors.Is(err, ErrNotInstalled) {
		t.Errorf("without chrome.dll: %v", err)
	}
}

func TestState(t *testing.T) {
	root := t.TempDir()
	s, err := LoadState(root)
	if err != nil || !reflect.DeepEqual(s, State{}) {
		t.Fatalf("LoadState of a new install = %+v, %v", s, err)
	}
	s.Channel = omaha.Beta
	at := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	for i := range historyLimit + 5 {
		s.Record(Event{Time: at.Add(time.Duration(i) * time.Minute), Kind: ChromeUpdated, To: "156.0.8078.74"})
	}
	if len(s.History) != historyLimit || !s.History[0].Time.Equal(at.Add((historyLimit+4)*time.Minute)) {
		t.Fatalf("history: %d events, newest %v", len(s.History), s.History[0].Time)
	}
	if err := SaveState(root, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(root)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("LoadState = %+v, %v", got, err)
	}
	os.WriteFile(filepath.Join(Meta(root), stateFile), []byte("{"), 0o644)
	if _, err := LoadState(root); err == nil {
		t.Error("read a broken state file")
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"156.0.8078.74", "156.0.8078.48", 1},
		{"156.0.8078.9", "156.0.8078.48", -1},
		{"155.0.9999.99", "156.0.0.0", -1},
		{"1.18.3", "1.18.3", 0},
		{"1.18", "1.18.0", 0},
		{"1.9", "1.18", -1},
	} {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%s, %s) = %d", tc.a, tc.b, got)
		}
	}
}
