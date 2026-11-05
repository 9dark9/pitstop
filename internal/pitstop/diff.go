package pitstop

import (
	"context"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/Bush2021/chrome_update/fetch"
	"github.com/Bush2021/chrome_update/omaha"
	"github.com/Bush2021/chrome_update/unpack"
	"github.com/Bush2021/chrome_update/zucc"
	"github.com/Bush2021/pitstop/internal/install"
)

// baseCache holds the package of the last Chrome update, when the install
// takes differential updates. Like Chrome's updater, Pitstop reports its
// SHA-256 to the update server, which can then answer with a Zucchini
// patch from it.
const baseCache = chromeCache + "/base"

func isSHA256(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32
}

// base returns the SHA-256 of the kept package. Its name says it; zucc
// checks the package again, and a mismatch falls back to the full one.
func (e *Engine) base() (string, bool) {
	entries, _ := os.ReadDir(e.path(baseCache))
	for _, en := range entries {
		if sum, ok := strings.CutSuffix(en.Name(), ".crx3"); ok && en.Type().IsRegular() && isSHA256(sum) {
			return strings.ToLower(sum), true
		}
	}
	return "", false
}

// pickDiff returns a differential pipeline from a cached package: download,
// an optional xz, zucc and crx3. Given the full pipeline, it must make the
// same package with a smaller download.
func pickDiff(pipelines []omaha.Pipeline, cached []string, full omaha.Pipeline, hasFull bool) (omaha.Pipeline, bool) {
	fullDL, _ := full.Download()
	fullPkg, _ := full.Package()
	for _, p := range pipelines {
		types := make([]string, len(p.Ops))
		for i, op := range p.Ops {
			types[i] = op.Type
		}
		shape := strings.Join(types, ",")
		if p.Kind() != omaha.OpZucc || shape != "download,zucc,crx3" && shape != "download,xz,zucc,crx3" {
			continue
		}
		dl, pkg, step := p.Ops[0], p.Ops[len(p.Ops)-1], p.Ops[len(p.Ops)-2]
		if !isSHA256(dl.Out) || dl.Size <= 0 || !isSHA256(pkg.In) || pkg.Size <= 0 ||
			!strings.EqualFold(step.Out, pkg.In) || !slices.Contains(cached, strings.ToLower(step.Previous)) {
			continue
		}
		if hasFull && (!strings.EqualFold(pkg.In, fullPkg.In) || dl.Size >= fullDL.Size) {
			continue
		}
		return p, true
	}
	return omaha.Pipeline{}, false
}

// diffChrome downloads the pipeline's patch, decoding an xz stream as it
// arrives, and applies it to the kept package, making crx.
func (e *Engine) diffChrome(ctx context.Context, p omaha.Pipeline, crx string) error {
	dl, pkg, step := p.Ops[0], p.Ops[len(p.Ops)-1], p.Ops[len(p.Ops)-2]
	download := e.path(chromeCache, dl.Out+".zuc")
	file := fetch.File{URLs: dl.URLs, Size: dl.Size, SHA256: dl.Out, Dest: download}
	patch := download
	if len(p.Ops) == 4 {
		file.Dest = e.path(chromeCache, dl.Out+".xz")
		patch = e.path(chromeCache, pkg.In+".zuc")
		var downloaded atomic.Bool
		decoding := e.report(Decoding)
		file.Follow = func(r io.Reader) error {
			return unpack.DecodeXZ(ctx, r, patch, "", 0, func(done, total int64) {
				if downloaded.Load() {
					decoding(done, total)
				}
			})
		}
		defer os.Remove(file.Dest)
		downloading := e.report(Downloading)
		if err := fetch.Get(ctx, e.HTTP, file, func(done, total int64) {
			downloading(done, total)
			downloaded.Store(done == total)
		}); err != nil {
			return err
		}
	} else if err := fetch.Get(ctx, e.HTTP, file, e.report(Downloading)); err != nil {
		return err
	}
	defer os.Remove(patch)
	e.report(Diffing)(0, 0)
	return zucc.Apply(e.path(baseCache, strings.ToLower(step.Previous)+".crx3"), patch, crx, pkg.Size, pkg.In)
}

// keepBase keeps the staged build's package crx for the next update's
// patch, in place of the one before, or deletes it. Failing to keep it
// only costs the next update a full download.
func (e *Engine) keepBase(crx string) {
	s, err := install.LoadState(e.Root)
	if err != nil || !s.DiffUpdates {
		os.Remove(crx)
		os.RemoveAll(e.path(baseCache))
		return
	}
	name := filepath.Base(crx)
	if os.MkdirAll(e.path(baseCache), 0o755) != nil || os.Rename(crx, e.path(baseCache, name)) != nil {
		os.Remove(crx)
		return
	}
	e.prune(baseCache, func(n string) bool { return n == name })
}
