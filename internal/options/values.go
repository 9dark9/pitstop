package options

import (
	"path/filepath"
	"strings"
)

// SplitSwitches splits a command_line value where CommandLineToArgvW does:
// at white space outside double quotes. Each switch stays as written.
func SplitSwitches(v string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range v {
		switch {
		case r == '"':
			quoted = !quoted
		case !quoted && (r == ' ' || r == '\t'):
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func JoinSwitches(list []string) string { return strings.Join(list, " ") }

// SplitCommands splits a launch_on_* value at semicolons. A command keeps
// its spaces, or a field editing it could not take the space after a word.
func SplitCommands(v string) []string {
	var out []string
	for c := range strings.SplitSeq(v, ";") {
		if strings.TrimSpace(c) != "" {
			out = append(out, c)
		}
	}
	return out
}

// JoinCommands ends each command in a semicolon: Chrome++ reads the line
// through GetPrivateProfileStringW, which would drop the outer quotes of a
// value that starts and ends with one.
func JoinCommands(list []string) string {
	var b strings.Builder
	for _, c := range list {
		if strings.TrimSpace(c) != "" {
			b.WriteString(c + ";")
		}
	}
	return b.String()
}

// SplitNames reads new_tab_disable_name as Chrome++ does: names separated
// by commas, each trimmed of quotes.
func SplitNames(v string) []string {
	var out []string
	for n := range strings.SplitSeq(v, ",") {
		if n = strings.Trim(strings.TrimSpace(n), `"`); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func JoinNames(list []string) string {
	quoted := make([]string, len(list))
	for i, n := range list {
		quoted[i] = `"` + n + `"`
	}
	return strings.Join(quoted, ",")
}

// InstallPath names path through %app%, which Chrome++ expands, when it is
// in the install that has its Chrome in app, so that the folder can move.
func InstallPath(app, path string) string {
	if rel, err := filepath.Rel(app, path); err == nil && !outside(rel) {
		return `%app%\` + rel
	}
	if rel, err := filepath.Rel(filepath.Dir(app), path); err == nil && !outside(rel) {
		return `%app%\..\` + rel
	}
	return path
}

func outside(rel string) bool {
	return rel == "." || rel == ".." || strings.HasPrefix(rel, `..\`)
}
