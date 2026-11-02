//go:build !windows

package verify

func Signer(string) (string, error) { return "", ErrUnsupported }
