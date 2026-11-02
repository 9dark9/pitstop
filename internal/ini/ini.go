// Package ini edits chrome++.ini without losing a byte it does not change.
// Comments, blank lines, order, unknown keys, spacing, the encoding and the
// line endings all stay; an edit rewrites only the value it changes.
//
// It reads the file the way Windows' profile API does, which is how
// Chrome++ reads it: names compare without case, spaces around names and
// values do not count, only the first section of a name is searched, the
// first key of a name in it wins, and a value in matching quotes loses
// them. A line starting with ";" is a comment.
package ini

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type Encoding int

const (
	// ANSI has no byte order mark: Windows reads it in the system's ANSI
	// code page, so only ASCII is safe to write.
	ANSI Encoding = iota
	UTF8
	UTF16LE
	UTF16BE
)

var boms = map[Encoding][]byte{
	UTF8:    {0xEF, 0xBB, 0xBF},
	UTF16LE: {0xFF, 0xFE},
	UTF16BE: {0xFE, 0xFF},
}

func (e Encoding) String() string {
	return [...]string{"ANSI", "UTF-8", "UTF-16LE", "UTF-16BE"}[e]
}

type kind int

const (
	other kind = iota // blank, or text Windows ignores
	comment
	section
	key
)

// line is one line of the file: its encoded bytes, kept as they were
// until an edit, and their meaning.
type line struct {
	raw                                      []byte // encoded, without the line ending
	eol                                      []byte // encoded line ending; empty on a last line without one
	text                                     string // decoded
	kind                                     kind
	name                                     string
	nameStart, nameEnd, valueStart, valueEnd int
}

type File struct {
	enc   Encoding
	lines []line
	// eol is the line ending new lines get: the file's first one.
	eol string
}

// Parse reads b. Any bytes parse: what Windows would ignore stays as it is.
func Parse(b []byte) *File {
	f := &File{enc: ANSI, eol: "\r\n"}
	for _, e := range []Encoding{UTF8, UTF16LE, UTF16BE} {
		if bytes.HasPrefix(b, boms[e]) {
			f.enc = e
			b = b[len(boms[e]):]
			break
		}
	}
	unit := 1
	if f.enc == UTF16LE || f.enc == UTF16BE {
		unit = 2
		if len(b)%2 == 1 {
			b = append(b[:len(b):len(b)], 0) // Windows drops an odd last byte too
		}
	}
	first := true
	for len(b) > 0 {
		i := 0
		for ; i < len(b); i += unit {
			if c := f.unitAt(b, i); c == '\n' || c == '\r' {
				break
			}
		}
		j := i
		if j < len(b) && f.unitAt(b, j) == '\r' {
			j += unit
		}
		if j < len(b) && f.unitAt(b, j) == '\n' && (j == i || f.unitAt(b, i) == '\r') {
			j += unit
		}
		l := line{raw: b[:i], eol: b[i:j]}
		if first && j > i {
			f.eol = f.decode(b[i:j])
			first = false
		}
		f.parseLine(&l)
		f.lines = append(f.lines, l)
		b = b[j:]
	}
	return f
}

func (f *File) unitAt(b []byte, i int) uint16 {
	switch f.enc {
	case UTF16LE:
		return binary.LittleEndian.Uint16(b[i:])
	case UTF16BE:
		return binary.BigEndian.Uint16(b[i:])
	}
	return uint16(b[i])
}

func (f *File) decode(b []byte) string {
	switch f.enc {
	case UTF16LE, UTF16BE:
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = f.unitAt(b, 2*i)
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}

func (f *File) encode(s string) ([]byte, error) {
	switch f.enc {
	case UTF16LE, UTF16BE:
		u := utf16.Encode([]rune(s))
		b := make([]byte, 2*len(u))
		for i, c := range u {
			if f.enc == UTF16LE {
				binary.LittleEndian.PutUint16(b[2*i:], c)
			} else {
				binary.BigEndian.PutUint16(b[2*i:], c)
			}
		}
		return b, nil
	case ANSI:
		for i := range len(s) {
			if s[i] >= utf8.RuneSelf {
				return nil, fmt.Errorf("%q: %w", s, ErrNonASCII)
			}
		}
	}
	return []byte(s), nil
}

// isSpace is Windows' idea of a space in a profile file.
func isSpace(c byte) bool { return c >= 0x09 && c <= 0x0d || c == 0x1a || c == ' ' }

func trim(s string, start, end int) (int, int) {
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return start, end
}

func (f *File) parseLine(l *line) {
	l.text = f.decode(l.raw)
	s := l.text
	start, end := trim(s, 0, len(s))
	switch {
	case start == end:
		l.kind = other
	case s[start] == ';':
		l.kind = comment
	case s[start] == '[' && strings.LastIndexByte(s[start:end], ']') > 0:
		close := start + strings.LastIndexByte(s[start:end], ']')
		ns, ne := trim(s, start+1, close)
		l.kind, l.name = section, s[ns:ne]
	case strings.IndexByte(s[start:end], '=') >= 0:
		eq := start + strings.IndexByte(s[start:end], '=')
		ns, ne := trim(s, start, eq)
		vs, _ := trim(s, eq+1, end)
		l.kind, l.name = key, s[ns:ne]
		l.nameStart, l.nameEnd, l.valueStart, l.valueEnd = ns, ne, vs, end
	default:
		l.kind = other
	}
}

func (f *File) Encoding() Encoding { return f.enc }

func (f *File) Bytes() []byte {
	var b bytes.Buffer
	b.Write(boms[f.enc])
	for _, l := range f.lines {
		b.Write(l.raw)
		b.Write(l.eol)
	}
	return b.Bytes()
}

func (f *File) Text() string {
	var b strings.Builder
	for _, l := range f.lines {
		b.WriteString(l.text)
		b.WriteString(f.decode(l.eol))
	}
	return b.String()
}

func (f *File) Lines() []string {
	out := make([]string, len(f.lines))
	for i, l := range f.lines {
		out[i] = l.text
	}
	return out
}

// sectionAt returns the index of the header of the first section named
// name, or -1. Keys before any header belong to the unnamed section "".
func (f *File) sectionAt(name string) int {
	for i, l := range f.lines {
		if l.kind == section && strings.EqualFold(l.name, name) {
			return i
		}
	}
	return -1
}

func (f *File) span(name string) (start, end int, ok bool) {
	start = -1
	if name != "" {
		if start = f.sectionAt(name); start < 0 {
			return 0, 0, false
		}
	}
	end = start + 1
	for end < len(f.lines) && f.lines[end].kind != section {
		end++
	}
	return start, end, true
}

func (f *File) find(sec, name string) int {
	start, end, ok := f.span(sec)
	if !ok {
		return -1
	}
	for i := start + 1; i < end; i++ {
		if l := f.lines[i]; l.kind == key && strings.EqualFold(l.name, name) {
			return i
		}
	}
	return -1
}

// Raw returns a key's value as written, quotes included.
func (f *File) Raw(sec, name string) (string, bool) {
	i := f.find(sec, name)
	if i < 0 {
		return "", false
	}
	l := f.lines[i]
	return l.text[l.valueStart:l.valueEnd], true
}

// Get returns a key's value as Chrome++ reads it: without the quotes of a
// value in matching quotes.
func (f *File) Get(sec, name string) (string, bool) {
	v, ok := f.Raw(sec, name)
	return Unquote(v), ok
}

// Unquote drops the quotes around a value in matching single or double
// quotes, as Windows' profile API does.
func Unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

type KeyValue struct{ Key, Value string }

func (f *File) Keys(sec string) []KeyValue {
	start, end, ok := f.span(sec)
	if !ok {
		return nil
	}
	var out []KeyValue
	for i := start + 1; i < end; i++ {
		if l := f.lines[i]; l.kind == key {
			out = append(out, KeyValue{l.name, l.text[l.valueStart:l.valueEnd]})
		}
	}
	return out
}

var (
	ErrLineBreak = errors.New("a value in chrome++.ini cannot hold a line break")
	// ErrNonASCII is for a file without a byte order mark, which Windows
	// reads in the system's code page.
	ErrNonASCII = errors.New("a chrome++.ini without a byte order mark can hold only ASCII characters")
)

func (f *File) setText(i int, text string) error {
	raw, err := f.encode(text)
	if err != nil {
		return err
	}
	f.lines[i].raw = raw
	f.parseLine(&f.lines[i])
	return nil
}

// Set writes a key's value as given, quotes included. An existing key keeps
// its spacing and its line; a new one goes after the last key of the first
// section named sec, which is added at the end when there is none.
func (f *File) Set(sec, name, value string) error {
	if strings.ContainsAny(value+name+sec, "\r\n") {
		return ErrLineBreak
	}
	if i := f.find(sec, name); i >= 0 {
		l := f.lines[i]
		return f.setText(i, l.text[:l.valueStart]+value+l.text[l.valueEnd:])
	}
	start, end, ok := f.span(sec)
	if !ok {
		f.terminate()
		if len(f.lines) > 0 && strings.TrimSpace(f.lines[len(f.lines)-1].text) != "" {
			if err := f.insert(len(f.lines), ""); err != nil {
				return err
			}
		}
		if err := f.insert(len(f.lines), "["+sec+"]"); err != nil {
			return err
		}
		return f.insert(len(f.lines), name+"="+value)
	}
	at := start + 1
	for i := start + 1; i < end; i++ {
		if f.lines[i].kind == key {
			at = i + 1
		}
	}
	if at == len(f.lines) {
		f.terminate()
	}
	return f.insert(at, name+"="+value)
}

func (f *File) terminate() {
	if n := len(f.lines); n > 0 && len(f.lines[n-1].eol) == 0 {
		f.lines[n-1].eol, _ = f.encode(f.eol)
	}
}

func (f *File) insert(at int, text string) error {
	raw, err := f.encode(text)
	if err != nil {
		return err
	}
	eol, _ := f.encode(f.eol)
	l := line{raw: raw, eol: eol}
	f.parseLine(&l)
	f.lines = append(f.lines[:at], append([]line{l}, f.lines[at:]...)...)
	return nil
}

func (f *File) Sections() []string {
	var out []string
	for i, l := range f.lines {
		if l.kind == section && f.sectionAt(l.name) == i {
			out = append(out, l.name)
		}
	}
	return out
}

// SetKeys makes the keys of the first section named sec the list kvs, as
// the [keymapping] section is a list: the key lines there are rewritten in
// place, in order, those beyond kvs deleted, and those missing added after
// the last. Comments stay where they are.
func (f *File) SetKeys(sec string, kvs []KeyValue) error {
	for _, kv := range kvs {
		if strings.ContainsAny(kv.Key+kv.Value, "\r\n") {
			return ErrLineBreak
		}
	}
	start, end, ok := f.span(sec)
	if !ok {
		if len(kvs) == 0 {
			return nil
		}
		if err := f.Set(sec, kvs[0].Key, kvs[0].Value); err != nil {
			return err
		}
		start, end, _ = f.span(sec)
		kvs = kvs[1:]
		for i := start + 1; i < end; i++ {
			if f.lines[i].kind == key {
				start = i
			}
		}
	}
	var keys []int
	for i := start + 1; i < end; i++ {
		if f.lines[i].kind == key {
			keys = append(keys, i)
		}
	}
	for n, i := range keys {
		if n >= len(kvs) {
			break
		}
		l := f.lines[i]
		if l.name == kvs[n].Key && l.text[l.valueStart:l.valueEnd] == kvs[n].Value {
			continue
		}
		text := l.text[:l.nameStart] + kvs[n].Key + l.text[l.nameEnd:l.valueStart] + kvs[n].Value + l.text[l.valueEnd:]
		if err := f.setText(i, text); err != nil {
			return err
		}
	}
	for n := len(keys) - 1; n >= len(kvs); n-- {
		f.lines = append(f.lines[:keys[n]], f.lines[keys[n]+1:]...)
	}
	if len(kvs) <= len(keys) {
		return nil
	}
	at := start + 1
	if len(keys) > 0 {
		at = keys[len(keys)-1] + 1
	}
	for _, kv := range kvs[len(keys):] {
		if at == len(f.lines) {
			f.terminate()
		}
		if err := f.insert(at, kv.Key+"="+kv.Value); err != nil {
			return err
		}
		at++
	}
	return nil
}

func (f *File) Delete(sec, name string) bool {
	i := f.find(sec, name)
	if i < 0 {
		return false
	}
	f.lines = append(f.lines[:i], f.lines[i+1:]...)
	return true
}

func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b), nil
}

// FromText parses text typed in an editor and stores it the way f is
// stored: same encoding, and f's line ending for every line.
func (f *File) FromText(text string) (*File, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\n", f.eol)
	b, err := f.encode(text)
	if err != nil {
		return nil, err
	}
	return Parse(append(append([]byte{}, boms[f.enc]...), b...)), nil
}

// Save writes f to path, through a temporary file in the same folder, and
// keeps the file it replaces as path+".bak".
func (f *File) Save(path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(f.Bytes())
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if old, rerr := os.ReadFile(path); rerr == nil {
			err = os.WriteFile(path+".bak", old, 0o644)
		}
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
