package verify

import (
	"os"
	"path/filepath"
	"testing"
)

// signedChrome finds a chrome.exe signed by Google: PITSTOP_TEST_SIGNED, or an
// installed Chrome.
func signedChrome(t *testing.T) string {
	candidates := []string{os.Getenv("PITSTOP_TEST_SIGNED")}
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		if dir := os.Getenv(env); dir != "" {
			candidates = append(candidates, filepath.Join(dir, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("set PITSTOP_TEST_SIGNED to a chrome.exe from Google")
	return ""
}

func TestSigner(t *testing.T) {
	exe := signedChrome(t)
	got, err := Signer(exe)
	if err != nil || got != Google {
		t.Fatalf("Signer(%s) = %q, %v", exe, got, err)
	}
	if err := SignedBy(Google, exe); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2]++
	bad := filepath.Join(t.TempDir(), "chrome.exe")
	os.WriteFile(bad, b, 0o644)
	if _, err := Signer(bad); err == nil {
		t.Error("a changed chrome.exe passed")
	}
	unsigned := filepath.Join(t.TempDir(), "unsigned.exe")
	os.WriteFile(unsigned, []byte("MZ"), 0o644)
	if _, err := Signer(unsigned); err == nil {
		t.Error("an unsigned file passed")
	}
}
