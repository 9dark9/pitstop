// Package apply changes an install's files so that a crash at any point
// leaves either the old files or the new ones. A transaction is a list of
// renames within one folder, written to a journal before the first rename:
// until the journal says the transaction committed, recovery renames every
// file back; once it has, recovery finishes removing what the transaction
// replaced.
package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Move renames From to To. Both are relative to the transaction's root, with
// forward slashes, so that a journal still applies after the folder moves,
// as a portable install on a USB drive does.
type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Tx struct {
	Op    string `json:"op"`
	Moves []Move `json:"moves"`
	// Remove lists what to delete once the transaction has committed, such
	// as the build it replaced.
	Remove []string `json:"remove,omitempty"`
	// Committed is set once every move is done; recovery then only removes.
	Committed bool `json:"committed,omitempty"`
}

// Run makes the moves under root, journaled at journal, then removes
// tx.Remove. When a move fails, the moves done so far are undone and the
// error returned.
func Run(root, journal string, tx Tx) error {
	if _, err := os.Stat(journal); err == nil {
		return fmt.Errorf("an unfinished change is pending in %s", journal)
	}
	tx.Committed = false
	if err := write(journal, tx); err != nil {
		return err
	}
	done := 0
	var err error
	for _, m := range tx.Moves {
		if err = move(root, m.From, m.To); err != nil {
			break
		}
		done++
	}
	if err != nil {
		if uerr := undo(root, tx.Moves[:done]); uerr != nil {
			return fmt.Errorf("%w; undoing it failed too, so the change stays pending: %v", err, uerr)
		}
		if rerr := os.Remove(journal); rerr != nil {
			return fmt.Errorf("%w; %v", err, rerr)
		}
		return err
	}
	tx.Committed = true
	if err := write(journal, tx); err != nil {
		return err
	}
	return finish(root, journal, tx)
}

// Recover completes or undoes the transaction a crash left in journal. It
// returns the transaction's description and whether one was pending.
func Recover(root, journal string) (op string, committed bool, err error) {
	b, err := os.ReadFile(journal)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var tx Tx
	if err := json.Unmarshal(b, &tx); err != nil {
		return "", false, fmt.Errorf("%s: %w", filepath.Base(journal), err)
	}
	if tx.Committed {
		return tx.Op, true, finish(root, journal, tx)
	}
	if err := undo(root, tx.Moves); err != nil {
		return tx.Op, false, err
	}
	return tx.Op, false, os.Remove(journal)
}

func Pending(journal string) bool {
	_, err := os.Stat(journal)
	return err == nil
}

func finish(root, journal string, tx Tx) error {
	for _, p := range tx.Remove {
		if err := removeAll(abs(root, p)); err != nil {
			return err
		}
	}
	return os.Remove(journal)
}

// undo renames each moved file back, newest first. A move whose To is gone
// or whose From is back was not done, or was already undone.
func undo(root string, moves []Move) error {
	for _, m := range slices.Backward(moves) {
		from, to := abs(root, m.From), abs(root, m.To)
		if !exists(to) || exists(from) {
			continue
		}
		if err := move(root, m.To, m.From); err != nil {
			return err
		}
	}
	return nil
}

func move(root, from, to string) error {
	dst := abs(root, to)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return retry(func() error { return os.Rename(abs(root, from), dst) })
}

func removeAll(path string) error { return retry(func() error { return os.RemoveAll(path) }) }

// retry repeats op while it fails the way a file briefly held open by an
// antivirus scan or the search indexer makes it fail on Windows.
func retry(op func() error) error {
	deadline := time.Now().Add(retryFor)
	for {
		err := op()
		if err == nil || !transient(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

const retryFor = 2 * time.Second

func abs(root, rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func write(journal string, tx Tx) error {
	b, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		return err
	}
	tmp := journal + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = retry(func() error { return os.Rename(tmp, journal) })
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
