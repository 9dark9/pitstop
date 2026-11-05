// Package pitstoptest fakes what Pitstop talks to, for tests: Google's
// update server and download hosts, GitHub's API with Chrome++ 1.18.3, and
// Chrome builds small enough to make in a test.
package pitstoptest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	zucchini "github.com/404Setup/go-zucchini"

	"github.com/Bush2021/chrome_update/pefile/pefiletest"
	"github.com/Bush2021/chrome_update/unpack"
	"github.com/Bush2021/chrome_update/unpack/unpacktest"
	"github.com/Bush2021/pitstop/internal/install"
)

const PlusArchive = "Chrome++_v1.18.3_x86_x64_arm64.7z"

// Fixture returns the path of a file in the repository, from its root: the
// folder above the test's that holds go.mod. Tests run in their package's
// folder, from go test or as a test binary copied to another machine.
func Fixture(rel string) string {
	dir, _ := os.Getwd()
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return filepath.Join(d, filepath.FromSlash(rel))
		}
		if filepath.Dir(d) == d {
			return filepath.Join(dir, filepath.FromSlash(rel))
		}
	}
}

func Sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// Package stands in for Google's CRX3 of a version; Unpack reads the
// version back from it.
func Package(version string) []byte {
	return bytes.Repeat([]byte("package of Chrome "+version+"\n"), 2000)
}

func Version(v string) (out [4]uint16) {
	fmt.Sscanf(v, "%d.%d.%d.%d", &out[0], &out[1], &out[2], &out[3])
	return out
}

// WriteBuild lays out an x64 Chrome build in dir, which may exist, as the
// installer's Chrome-bin folder holds it: a chrome.exe importing imports,
// chrome_proxy.exe and the version folder.
func WriteBuild(t testing.TB, dir, version string, imports ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, version), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := pefiletest.Image{
		Machine:     pe.IMAGE_FILE_MACHINE_AMD64,
		FileVersion: Version(version),
		Strings:     map[string]string{"FileDescription": "Google Chrome"},
		Imports:     append(imports, "KERNEL32.dll"),
	}
	if err := exe.Write(filepath.Join(dir, "chrome.exe")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "chrome_proxy.exe"), []byte("proxy "+version), 0o644)
	os.WriteFile(filepath.Join(dir, version, "chrome.dll"), []byte("chrome.dll "+version), 0o644)
}

// PatchWithSetDLL makes app look as setdll leaves it: Chrome++ renamed to
// version-x64.dll, which chrome.exe imports.
func PatchWithSetDLL(t testing.TB, app, version string) {
	t.Helper()
	if err := os.Rename(filepath.Join(app, "version.dll"), filepath.Join(app, "version-x64.dll")); err != nil {
		t.Fatal(err)
	}
	WriteBuild(t, app, version, "version-x64.dll")
}

func WriteChromePlus(t testing.TB, path, release string) {
	t.Helper()
	writeChromePlus(t, path, map[string]string{"ProductVersion": release})
}

// WriteManagedChromePlus writes a Chrome++ that reads managed_updates. A
// version string holds the key in UTF-16, as the real DLL's literal does.
func WriteManagedChromePlus(t testing.TB, path, release string) {
	t.Helper()
	writeChromePlus(t, path, map[string]string{"ProductVersion": release, "Comments": install.ManagedUpdatesKey})
}

func writeChromePlus(t testing.TB, path string, strs map[string]string) {
	t.Helper()
	strs["FileDescription"] = "Chrome++ Next"
	dll := pefiletest.Image{Machine: pe.IMAGE_FILE_MACHINE_AMD64, Strings: strs}
	if err := dll.Write(path); err != nil {
		t.Fatal(err)
	}
}

// Unpack unpacks a Package as the real unpacker unpacks Google's: into a
// folder it makes, whose parent must exist.
func Unpack(t testing.TB) func(ctx context.Context, crx3, dir string, progress unpack.Progress) (string, error) {
	return func(_ context.Context, crx3, dir string, _ unpack.Progress) (string, error) {
		b, err := os.ReadFile(crx3)
		if err != nil {
			return "", err
		}
		version := strings.TrimPrefix(strings.SplitN(string(b), "\n", 2)[0], "package of Chrome ")
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "", err
		}
		WriteBuild(t, dir, version)
		return version, nil
	}
}

// Servers fakes Google's update server, its download hosts and GitHub's
// API, over TLS: the update client keeps only https download URLs.
type Servers struct {
	*httptest.Server
	// Versions are the Chrome builds on offer, oldest first.
	Versions []string
	// Digest is the Chrome++ archive's digest in GitHub's API; "" for
	// none.
	Digest string
	// BadDiff has the differential updates it offers fail to apply.
	BadDiff bool
	// Gets counts the package downloads, and Diffs the differential ones
	// among them.
	Gets  atomic.Int32
	Diffs atomic.Int32
	// Cached is what the last update check reported as cached.
	Cached  []string
	mu      sync.Mutex
	files   map[string][]byte
	patches map[string]bool
}

func (s *Servers) serve(b []byte) (dl string, size int64) {
	x := unpacktest.XZ(b)
	dl = Sum(x)
	s.mu.Lock()
	s.files[dl] = x
	s.mu.Unlock()
	return dl, int64(len(x))
}

func (s *Servers) xzPackage(version string) (pkg []byte, dl string, size int64) {
	pkg = Package(version)
	dl, size = s.serve(pkg)
	return pkg, dl, size
}

// zuccPipeline patches a cached package of an offered version into next's,
// as Google's server does when the client accepts zucc.
func (s *Servers) zuccPipeline(cached []string, next string) (map[string]any, bool) {
	for _, v := range s.Versions {
		old := Package(v)
		if v == next || !slices.Contains(cached, Sum(old)) {
			continue
		}
		p, err := zucchini.GenerateBuffer(old, Package(next))
		if err != nil {
			return nil, false
		}
		if s.BadDiff {
			p[len(p)-1] ^= 0xff
		}
		dl, size := s.serve(p)
		s.mu.Lock()
		s.patches[dl] = true
		s.mu.Unlock()
		pkg := Package(next)
		return map[string]any{
			"pipeline_id": "1:zucc:" + Sum(pkg) + ":" + Sum(old),
			"operations": []any{
				map[string]any{"type": "download", "size": size, "out": map[string]string{"sha256": dl},
					"urls": []any{map[string]string{"url": s.URL + "/dl/" + dl}}},
				map[string]any{"type": "xz"},
				map[string]any{"type": "zucc", "previous": map[string]string{"sha256": Sum(old)}, "out": map[string]string{"sha256": Sum(pkg)}},
				map[string]any{"type": "crx3", "size": len(pkg), "in": map[string]string{"sha256": Sum(pkg)}},
			},
		}, true
	}
	return nil, false
}

func NewServers(t testing.TB, versions ...string) *Servers {
	s := &Servers{Versions: versions, files: map[string][]byte{}, patches: map[string]bool{}}
	archive, err := os.ReadFile(Fixture("testdata/" + PlusArchive))
	if err != nil {
		t.Fatal(err)
	}
	s.files[PlusArchive] = archive
	s.Digest = "sha256:" + Sum(archive)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /update", s.update)
	mux.HandleFunc("GET /dl/{name}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		b, ok := s.files[r.PathValue("name")]
		patch := s.patches[r.PathValue("name")]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.Gets.Add(1)
		if patch {
			s.Diffs.Add(1)
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
	})
	mux.HandleFunc("GET /repos/Bush2021/chrome_plus/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		var digest *string
		if s.Digest != "" {
			digest = &s.Digest
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "1.18.3", "published_at": "2026-10-26T20:00:00Z",
			"assets": []map[string]any{{
				"name": PlusArchive, "size": len(archive), "digest": digest,
				"browser_download_url": s.URL + "/dl/" + PlusArchive,
			}},
		})
	})
	s.Server = httptest.NewTLSServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *Servers) Endpoint() string { return s.URL + "/update" }

// update answers a protocol 4 request with the newest version, or the
// newest one matching the target prefix, as an xz pipeline, after a patch
// pipeline if it can make one.
func (s *Servers) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Request struct {
			AcceptFormat string `json:"acceptformat"`
			Apps         []struct {
				Cached []struct {
					SHA256 string `json:"sha256"`
				} `json:"cached_items"`
				Version     string `json:"version"`
				UpdateCheck struct {
					Target string `json:"targetversionprefix"`
				} `json:"updatecheck"`
			} `json:"apps"`
		} `json:"request"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a := req.Request.Apps[0]
	var cached []string
	for _, c := range a.Cached {
		cached = append(cached, c.SHA256)
	}
	s.mu.Lock()
	s.Cached = cached
	s.mu.Unlock()
	next := ""
	for _, v := range s.Versions {
		if strings.HasPrefix(v, a.UpdateCheck.Target) {
			next = v
		}
	}
	check := map[string]any{"status": "noupdate"}
	if next != "" && next != a.Version && (a.UpdateCheck.Target != "" || install.CompareVersions(next, a.Version) > 0) {
		pkg, dl, size := s.xzPackage(next)
		var pipelines []any
		if slices.Contains(strings.Split(req.Request.AcceptFormat, ","), "zucc") {
			if p, ok := s.zuccPipeline(cached, next); ok {
				pipelines = append(pipelines, p)
			}
		}
		check = map[string]any{
			"status": "ok", "nextversion": next,
			"pipelines": append(pipelines, map[string]any{
				"pipeline_id": "1:xz:" + Sum(pkg) + ":any",
				"operations": []any{
					map[string]any{"type": "download", "size": size, "out": map[string]string{"sha256": dl},
						"urls": []any{map[string]string{"url": s.URL + "/dl/" + dl}}},
					map[string]any{"type": "xz"},
					map[string]any{"type": "crx3", "size": len(pkg), "in": map[string]string{"sha256": Sum(pkg)}},
				},
			}),
		}
	}
	io.WriteString(w, ")]}'\n")
	json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{
		"apps": []any{map[string]any{"status": "ok", "updatecheck": check}},
	}})
}
