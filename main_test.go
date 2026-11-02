package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDefaultRoot(t *testing.T) {
	if want := os.Getenv("PITSTOP_TEST_ROOT"); want != "" {
		if got := defaultRoot(); got != want {
			t.Fatalf("defaultRoot() = %q, want %q", got, want)
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Chrome", "pitstop", "with-chrome"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), name)
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if name == "with-chrome" {
				if err := os.WriteFile(filepath.Join(root, "chrome.exe"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			child := filepath.Join(root, filepath.Base(exe))
			if err := os.WriteFile(child, binary, 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(child, "-test.run=^TestDefaultRoot$")
			cmd.Env = append(os.Environ(), "PITSTOP_TEST_ROOT="+root)
			cmd.Dir = t.TempDir()
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
