package pitstop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Bush2021/pitstop/internal/apply"
	"github.com/Bush2021/pitstop/internal/ini"
)

const (
	data   = "Data"
	backup = meta + "/backup"
)

// rebuilt names the folders of the user data folder, or of a profile in it,
// that Chrome rebuilds, so a copy of the profile leaves them out. Cache and
// Code Cache are in the install's Cache folder unless chrome++.ini clears
// cache_dir.
var rebuilt = map[string]bool{
	"cache":                          true,
	"code cache":                     true,
	"gpucache":                       true,
	"dawnwebgpucache":                true,
	"dawngraphitecache":              true,
	"shadercache":                    true,
	"grshadercache":                  true,
	"gpupersistentcache":             true,
	"component_crx_cache":            true,
	"extensions_crx_cache":           true,
	"optimization_guide_model_store": true,
	"optguideondevicemodel":          true,
}

// ProfileKept reports whether a copy of the profile that Chrome version
// used is kept, which a switch to that version puts back.
func (e *Engine) ProfileKept(version string) bool {
	return exists(e.path(backup, "data-"+version)) && e.ownsData()
}

// ownsData reports whether the user data folder is the install's Data. Only
// that one gets a copy: a folder elsewhere may be on another volume, where
// the transaction that swaps Chrome can't rename it.
func (e *Engine) ownsData() bool {
	f, err := ini.Load(e.path(app, "chrome++.ini"))
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	raw, _ := f.Raw("general", "data_dir")
	dir, ok := e.DataFolder(ini.Unquote(raw))
	return ok && strings.EqualFold(dir, e.path(data))
}

// backupProfile copies Data, minus what Chrome rebuilds, into
// backup\data-<version>, which replaces the copy kept so far. Chrome must be
// closed: a copy made while it started is discarded.
func (e *Engine) backupProfile(ctx context.Context, version string) error {
	if !exists(e.path(data)) || !e.ownsData() {
		return nil
	}
	name := "data-" + version
	partial := e.path(backup, name+".partial")
	if err := os.RemoveAll(partial); err != nil {
		return err
	}
	e.report(BackingUp)(0, 0)
	err := e.copyProfile(ctx, partial)
	if running := e.closed(); running != nil {
		err = running
	}
	if err == nil {
		err = e.prune(backup, func(n string) bool { return n == filepath.Base(partial) })
	}
	if err == nil {
		err = os.Rename(partial, e.path(backup, name))
	}
	if err != nil {
		os.RemoveAll(partial)
	}
	return err
}

// swapProfile returns the moves that put back the copy of the profile Chrome
// version to used, and keep the current one, of Chrome version from, in its
// place.
func (e *Engine) swapProfile(from, to string) []apply.Move {
	var moves []apply.Move
	if exists(e.path(data)) {
		moves = append(moves, apply.Move{From: data, To: backup + "/data-" + from})
	}
	return append(moves, apply.Move{From: backup + "/data-" + to, To: data})
}

func (e *Engine) copyProfile(ctx context.Context, dst string) error {
	return copyTree(ctx, e.path(data), dst, false, e.report(BackingUp))
}

// envVar is what ExpandEnvironmentStrings expands.
var envVar = regexp.MustCompile(`%[^%]+%`)

// DataFolder resolves a data_dir value as Chrome++ does. It reports false
// where no folder of the install's is meant: for none, which leaves the
// folder to Chrome, and a relative path, which depends on how Chrome started.
func (e *Engine) DataFolder(value string) (string, bool) {
	if value == "none" {
		return "", false
	}
	if value == "" {
		return e.path(data), true
	}
	value = strings.ReplaceAll(value, "%app%", e.path(app))
	value = envVar.ReplaceAllStringFunc(value, func(v string) string {
		if s, ok := os.LookupEnv(strings.Trim(v, "%")); ok {
			return s
		}
		return v
	})
	return filepath.Clean(value), filepath.IsAbs(value)
}

// MovableData reports whether there is something in from to move, and room
// for it in to.
func MovableData(from, to string) bool {
	if inside(from, to) || inside(to, from) {
		return false
	}
	held, err := os.ReadDir(from)
	if err != nil || len(held) == 0 {
		return false
	}
	held, err = os.ReadDir(to)
	return errors.Is(err, fs.ErrNotExist) || err == nil && len(held) == 0
}

func inside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// MoveData moves the user data folder. Chrome must be closed. If it fails,
// from is as it was, unless all of it reached to and part of it is left
// behind.
func (e *Engine) MoveData(ctx context.Context, from, to string) error {
	l, err := e.lock()
	if err != nil {
		return err
	}
	defer l.Unlock()
	if err := e.closed(); err != nil {
		return err
	}
	if !MovableData(from, to) {
		return fmt.Errorf("can't move %s to %s", from, to)
	}
	e.report(Moving)(0, 0)
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	if err := os.Remove(to); err != nil { // a rename wants it gone
		return err
	}
	if os.Rename(from, to) == nil {
		return nil
	}
	return e.moveByCopy(ctx, from, to)
}

// moveByCopy moves a folder across volumes, where a rename can't.
func (e *Engine) moveByCopy(ctx context.Context, from, to string) error {
	partial := to + ".partial"
	if err := os.RemoveAll(partial); err != nil {
		return err
	}
	err := copyTree(ctx, from, partial, true, e.report(Moving))
	if running := e.closed(); running != nil {
		err = running
	}
	if err == nil {
		err = os.Rename(partial, to)
	}
	if err != nil {
		os.RemoveAll(partial)
		return err
	}
	return os.RemoveAll(from)
}

// copyTree copies src to dst, which it creates. With all, it copies every
// file there is or fails. Otherwise it leaves out what Chrome rebuilds, and
// what isn't a file or a folder.
func copyTree(ctx context.Context, src, dst string, all bool, report func(done, total int64)) error {
	var dirs, files []string
	var total int64
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Only the user data folder and its profiles hold Chrome's caches.
			if !all && rel != "." && strings.Count(rel, string(filepath.Separator)) < 2 && rebuilt[strings.ToLower(d.Name())] {
				return fs.SkipDir
			}
			dirs = append(dirs, rel)
			return nil
		}
		if !d.Type().IsRegular() {
			if all {
				return fmt.Errorf("%s isn't a file Pitstop can move", p)
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, rel)
		total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	for _, rel := range dirs {
		if err := os.MkdirAll(filepath.Join(dst, rel), 0o755); err != nil {
			return err
		}
	}
	c := &counter{ctx: ctx, total: total, report: report}
	for _, rel := range files {
		if err := copyFile(filepath.Join(src, rel), filepath.Join(dst, rel), c); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, c *counter) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	c.r = in
	_, err = io.Copy(out, c)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// counter reads a file being copied, reports the bytes copied so far and
// stops once ctx is done.
type counter struct {
	ctx         context.Context
	r           io.Reader
	done, total int64
	report      func(done, total int64)
}

func (c *counter) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	c.done += int64(n)
	c.report(c.done, c.total)
	return n, err
}
