package ghrelease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The fixtures follow GitHub's release schema (@octokit/openapi-types
// 29.0.1): digest is "sha256:<hex>", or null for assets uploaded before
// GitHub recorded digests.
const latest = `{
 "tag_name": "1.18.3", "draft": false, "prerelease": false,
 "published_at": "2026-10-26T19:40:00Z", "body": "notes",
 "assets": [
  {"name": "Chrome++_v1.18.3_x86_x64_arm64.7z", "size": 181011,
   "digest": "sha256:bfcca14dd07fc9823a639f4a46b7c53ce11fc86ef1da19eab818c800cf49a159",
   "browser_download_url": "https://github.com/Bush2021/chrome_plus/releases/download/1.18.3/Chrome++_v1.18.3_x86_x64_arm64.7z"},
  {"name": "setdll.7z", "size": 500000, "digest": null,
   "browser_download_url": "https://github.com/Bush2021/chrome_plus/releases/download/1.18.3/setdll.7z"}
 ]}`

func server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/repos/Bush2021/chrome_plus/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(latest))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLatest(t *testing.T) {
	c := &Client{API: server(t).URL}
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "1.18.3" || r.Notes != "notes" {
		t.Errorf("%+v", r)
	}
	a, ok := r.Archive()
	if !ok {
		t.Fatal("no archive")
	}
	if sum, ok := a.SHA256(); !ok || sum != "bfcca14dd07fc9823a639f4a46b7c53ce11fc86ef1da19eab818c800cf49a159" {
		t.Errorf("digest %q", a.Digest)
	}
	if _, ok := r.Assets[1].SHA256(); ok {
		t.Error("a null digest gave a hash")
	}
}

func TestCompare(t *testing.T) {
	ordered := []string{"1.9.9", "1.16.0-alpha.1", "1.16.0-alpha.2", "1.16.0-alpha.10", "1.16.0", "v1.16.1", "1.18.3", "1.18.4", "2.0.0"}
	for i := range ordered {
		for j := range ordered {
			got := Compare(ordered[i], ordered[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}
