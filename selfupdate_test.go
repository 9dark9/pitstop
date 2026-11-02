package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestSelfUpdate(t *testing.T) {
	if scenario := os.Getenv("PITSTOP_SELF_UPDATE_TEST"); scenario != "" {
		probeSelfUpdate(t, scenario)
		return
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "probe.exe")
	flags := "-X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageUpdateFeed=https://example.invalid/update-windows-amd64.json -X github.com/egoist/mygo.packageUpdateKey=" + base64.StdEncoding.EncodeToString(pub)
	if out, err := exec.Command("go", "test", "-c", "-o", binary, "-ldflags", flags, ".").CombinedOutput(); err != nil {
		t.Fatalf("build probe: %v\n%s", err, out)
	}
	program, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"prerelease", "stable", "bad-signature", "current", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			exe := filepath.Join(t.TempDir(), "pitstop.exe")
			if err := os.WriteFile(exe, program, 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "-test.run=^TestSelfUpdate$", "-test.v")
			cmd.Env = append(os.Environ(), "PITSTOP_SELF_UPDATE_TEST="+scenario, "PITSTOP_SELF_UPDATE_KEY="+base64.StdEncoding.EncodeToString(key))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

type selfUpdateSource struct {
	manifest, archive []byte
	checks            int
	prereleases       bool
}

func (s *selfUpdateSource) Manifest(ctx context.Context, prereleases bool) (io.ReadCloser, error) {
	s.checks++
	s.prereleases = prereleases
	return io.NopCloser(bytes.NewReader(s.manifest)), ctx.Err()
}

func (s *selfUpdateSource) Open(ctx context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.archive)), ctx.Err()
}

func probeSelfUpdate(t *testing.T, scenario string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(exe)
	for _, name := range []string{"App/chrome.exe", "Data/Default/History", "Cache/entry", ".pitstop/state.json", "notes.txt"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const replacement = "new Pitstop executable"
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "pitstop.exe", Mode: 0o755, Size: int64(len(replacement))}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, replacement); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("PITSTOP_SELF_UPDATE_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	current, next := "1.0.0-rc.1", "1.0.0-rc.2"
	if scenario == "stable" {
		current, next = "1.0.0+local-build", "1.0.1"
	}
	if scenario == "current" {
		next = current
	}
	sum := sha256.Sum256(archive.Bytes())
	sig := ed25519.Sign(ed25519.PrivateKey(key), sum[:])
	if scenario == "bad-signature" {
		sig[0] ^= 1
	}
	manifest, err := json.Marshal(map[string]any{
		"version": next, "url": "archive", "size": archive.Len(), "signature": base64.StdEncoding.EncodeToString(sig),
	})
	if err != nil {
		t.Fatal(err)
	}
	source := &selfUpdateSource{manifest: manifest, archive: archive.Bytes()}
	mygo.Updater.SetSource(source)
	mygo.App.SetVersion(current)
	configureUpdates()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if scenario == "cancel" {
		cancel()
	}
	s := &selfUpdater{}
	err = s.install(ctx)
	wantError := scenario == "bad-signature" || scenario == "cancel"
	if (err != nil) != wantError {
		t.Fatalf("install error = %v", err)
	}
	updated := !wantError && scenario != "current"
	if s.installed != updated {
		t.Fatalf("installed = %v, want %v", s.installed, updated)
	}
	want := original
	if updated {
		want = []byte(replacement)
		if err := s.install(ctx); err != nil || source.checks != 1 {
			t.Fatalf("repeated install: %v, checks %d", err, source.checks)
		}
	}
	if source.prereleases != (scenario != "stable") {
		t.Fatalf("prereleases = %v", source.prereleases)
	}
	if got, err := os.ReadFile(exe); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("executable differs from the expected version: %v", err)
	}
	for _, name := range []string{"App/chrome.exe", "Data/Default/History", "Cache/entry", ".pitstop/state.json", "notes.txt"} {
		if got, err := os.ReadFile(filepath.Join(root, name)); err != nil || string(got) != name {
			t.Errorf("%s changed: %q, %v", name, got, err)
		}
	}
}
