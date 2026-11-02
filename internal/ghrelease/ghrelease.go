// Package ghrelease finds Chrome++ releases on GitHub.
package ghrelease

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const Repo = "Bush2021/chrome_plus"

type Release struct {
	Tag       string
	Published time.Time
	Notes     string
	Assets    []Asset
}

type Asset struct {
	Name   string
	Size   int64
	Digest string // as GitHub gives it, such as "sha256:…"; empty for uploads before June 2025
	URL    string
}

func (a Asset) SHA256() (string, bool) {
	hex, ok := strings.CutPrefix(a.Digest, "sha256:")
	return hex, ok && len(hex) == 64
}

// Archive returns the release's archive of every architecture,
// Chrome++_v<version>_x86_x64_arm64.7z.
func (r *Release) Archive() (Asset, bool) {
	want := "Chrome++_v" + r.Tag + "_x86_x64_arm64.7z"
	for _, a := range r.Assets {
		if a.Name == want {
			return a, true
		}
	}
	return Asset{}, false
}

type Client struct {
	HTTP *http.Client
	API  string
}

type apiRelease struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body"`
	Assets      []struct {
		Name               string  `json:"name"`
		Size               int64   `json:"size"`
		Digest             *string `json:"digest"`
		BrowserDownloadURL string  `json:"browser_download_url"`
	} `json:"assets"`
}

func (a apiRelease) release() *Release {
	r := &Release{Tag: a.TagName, Published: a.PublishedAt, Notes: a.Body}
	for _, as := range a.Assets {
		asset := Asset{Name: as.Name, Size: as.Size, URL: as.BrowserDownloadURL}
		if as.Digest != nil {
			asset.Digest = *as.Digest
		}
		r.Assets = append(r.Assets, asset)
	}
	return r
}

var ErrNoRelease = errors.New("no Chrome++ release found")

func (c *Client) Latest(ctx context.Context) (*Release, error) {
	var a apiRelease
	if err := c.get(ctx, "/repos/"+Repo+"/releases/latest", &a); err != nil {
		return nil, err
	}
	return a.release(), nil
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	api := cmp.Or(c.API, "https://api.github.com")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNoRelease
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("GitHub answered %s: %s", resp.Status, strings.TrimSpace(string(body[:min(len(body), 200)])))
	}
	return json.Unmarshal(body, v)
}

// Compare orders Chrome++ versions such as "1.18.3" and "1.16.0-alpha.1":
// numbers compare as numbers, and a prerelease sorts before its release.
// A leading "v" is ignored.
func Compare(a, b string) int {
	a, b = strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v")
	ac, apre, _ := strings.Cut(a, "-")
	bc, bpre, _ := strings.Cut(b, "-")
	if c := compareDotted(ac, bc); c != 0 {
		return c
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	}
	return compareDotted(apre, bpre)
}

func compareDotted(a, b string) int {
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(ap), len(bp)) {
		var x, y string
		if i < len(ap) {
			x = ap[i]
		}
		if i < len(bp) {
			y = bp[i]
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				return cmpInt(xn, yn)
			}
		case x != y:
			return strings.Compare(x, y)
		}
	}
	return 0
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	return 1
}
