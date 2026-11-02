// Package verify checks Authenticode signatures with Windows' own
// WinVerifyTrust. Pitstop checks the SHA-256 of every package first;
// the signatures on chrome.exe and chrome.dll are a second line of defense.
package verify

import (
	"errors"
	"fmt"
	"path/filepath"
)

const Google = "Google LLC"

var ErrUnsupported = errors.New("checking Authenticode signatures needs Windows")

func SignedBy(signer string, paths ...string) error {
	for _, p := range paths {
		got, err := Signer(p)
		if err != nil {
			return err
		}
		if got != signer {
			return fmt.Errorf("%s is signed by %q, not %q", filepath.Base(p), got, signer)
		}
	}
	return nil
}
