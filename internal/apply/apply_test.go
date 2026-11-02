package apply

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tree writes files (paths relative to root, "dir/" for an empty folder)
// and returns root.
func tree(t *testing.T, root string, files ...string) string {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if strings.HasSuffix(f, "/") {
			os.MkdirAll(p, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func list(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, _ := os.ReadFile(p)
		out = append(out, filepath.ToSlash(rel)+"="+string(b))
		return nil
	})
	slices.Sort(out)
	return out
}

// swap is an update of App from build 1 to build 2, as Pitstop makes it.
func swap() Tx {
	return Tx{
		Op: "chrome 1 to 2",
		Moves: []Move{
			{"App/chrome.exe", "meta/rollback/chrome-1/chrome.exe"},
			{"App/1", "meta/rollback/chrome-1/1"},
			{"meta/staging/chrome-2/chrome.exe", "App/chrome.exe"},
			{"meta/staging/chrome-2/2", "App/2"},
		},
		Remove: []string{"meta/rollback/chrome-0", "meta/staging/chrome-2"},
	}
}

var (
	before = []string{
		"App/1/chrome.dll=App/1/chrome.dll",
		"App/chrome.exe=App/chrome.exe",
		"App/version.dll=App/version.dll",
		"meta/rollback/chrome-0/chrome.exe=meta/rollback/chrome-0/chrome.exe",
		"meta/staging/chrome-2/2/chrome.dll=meta/staging/chrome-2/2/chrome.dll",
		"meta/staging/chrome-2/chrome.exe=meta/staging/chrome-2/chrome.exe",
	}
	after = []string{
		"App/2/chrome.dll=meta/staging/chrome-2/2/chrome.dll",
		"App/chrome.exe=meta/staging/chrome-2/chrome.exe",
		"App/version.dll=App/version.dll",
		"meta/rollback/chrome-1/1/chrome.dll=App/1/chrome.dll",
		"meta/rollback/chrome-1/chrome.exe=App/chrome.exe",
	}
)

func install(t *testing.T) (root, journal string) {
	root = tree(t, t.TempDir(), "App/chrome.exe", "App/1/chrome.dll", "App/version.dll",
		"meta/rollback/chrome-0/chrome.exe", "meta/staging/chrome-2/chrome.exe", "meta/staging/chrome-2/2/chrome.dll")
	return root, filepath.Join(root, "meta", "journal.json")
}

func TestRun(t *testing.T) {
	root, journal := install(t)
	if got := list(t, root); !slices.Equal(got, before) {
		t.Fatalf("fixture:\n%s", strings.Join(got, "\n"))
	}
	if err := Run(root, journal, swap()); err != nil {
		t.Fatal(err)
	}
	if got := list(t, root); !slices.Equal(got, after) {
		t.Fatalf("after Run:\n%s", strings.Join(got, "\n"))
	}
	for _, gone := range []string{"meta/staging/chrome-2", "meta/rollback/chrome-0"} {
		if exists(filepath.Join(root, gone)) {
			t.Errorf("%s is still there", gone)
		}
	}
}

func TestRunUndoesAFailedMove(t *testing.T) {
	root, journal := install(t)
	tx := swap()
	tx.Moves[3].From = "meta/staging/chrome-2/missing"
	if err := Run(root, journal, tx); err == nil {
		t.Fatal("Run succeeded without a file to move")
	}
	if got := list(t, root); !slices.Equal(got, before) {
		t.Fatalf("after the failed Run:\n%s", strings.Join(got, "\n"))
	}
}

func TestRunRefusesWhileAChangeIsPending(t *testing.T) {
	root, journal := install(t)
	os.WriteFile(journal, []byte(`{"op":"x","moves":[]}`), 0o644)
	if err := Run(root, journal, swap()); err == nil {
		t.Fatal("ran over a pending journal")
	}
	if got := list(t, root); len(got) != len(before)+1 {
		t.Fatalf("files changed:\n%s", strings.Join(got, "\n"))
	}
}

// TestRecoverAfterACrash stops a transaction after each move, as a crash
// would, and checks that recovery puts every file back.
func TestRecoverAfterACrash(t *testing.T) {
	tx := swap()
	for crashAfter := range len(tx.Moves) + 1 {
		root, journal := install(t)
		if err := write(journal, tx); err != nil {
			t.Fatal(err)
		}
		for _, m := range tx.Moves[:crashAfter] {
			if err := move(root, m.From, m.To); err != nil {
				t.Fatal(err)
			}
		}
		// The folder can move before the next start, as a USB drive does.
		moved := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.Rename(root, moved); err != nil {
			t.Fatal(err)
		}
		op, committed, err := Recover(moved, filepath.Join(moved, "meta", "journal.json"))
		if err != nil || op != tx.Op || committed {
			t.Fatalf("crash after %d moves: Recover = %q, %v, %v", crashAfter, op, committed, err)
		}
		if got := list(t, moved); !slices.Equal(got, before) {
			t.Fatalf("crash after %d moves, recovered:\n%s", crashAfter, strings.Join(got, "\n"))
		}
	}
}

func TestRecoverFinishesACommittedChange(t *testing.T) {
	root, journal := install(t)
	tx := swap()
	for _, m := range tx.Moves {
		move(root, m.From, m.To)
	}
	tx.Committed = true
	write(journal, tx)
	if _, committed, err := Recover(root, journal); err != nil || !committed {
		t.Fatalf("Recover = %v, %v", committed, err)
	}
	if got := list(t, root); !slices.Equal(got, after) {
		t.Fatalf("recovered:\n%s", strings.Join(got, "\n"))
	}
	if Pending(journal) {
		t.Error("the journal is still there")
	}
}

func TestRecoverWithoutAJournal(t *testing.T) {
	root := t.TempDir()
	if op, _, err := Recover(root, filepath.Join(root, "journal.json")); op != "" || err != nil {
		t.Fatalf("Recover = %q, %v", op, err)
	}
}
