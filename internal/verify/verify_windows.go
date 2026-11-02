package verify

import (
	"errors"
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	wintrust                = windows.NewLazySystemDLL("wintrust.dll")
	procProvDataFromState   = wintrust.NewProc("WTHelperProvDataFromStateData")
	procProvSignerFromChain = wintrust.NewProc("WTHelperGetProvSignerFromChain")
)

// providerSigner is the start of CRYPT_PROVIDER_SGNR.
type providerSigner struct {
	size       uint32
	verifyAsOf windows.Filetime
	chainLen   uint32
	chain      *providerCert
}

// providerCert is the start of CRYPT_PROVIDER_CERT.
type providerCert struct {
	size uint32
	cert *windows.CertContext
}

// Signer checks the Authenticode signature of path, offline, and returns
// the name of its signer, such as "Google LLC".
func Signer(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	file := windows.WinTrustFileInfo{Size: uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})), FilePath: p}
	data := windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&file),
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		ProvFlags:                       windows.WTD_CACHE_ONLY_URL_RETRIEVAL,
	}
	err = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	defer func() {
		data.StateAction = windows.WTD_STATEACTION_CLOSE
		windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	}()
	if err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	provData, _, _ := procProvDataFromState.Call(uintptr(data.StateData))
	if provData == 0 {
		return "", errors.New("WinVerifyTrust kept no signer")
	}
	sgnr, _, _ := procProvSignerFromChain.Call(provData, 0, 0, 0)
	if sgnr == 0 {
		return "", errors.New("WinVerifyTrust kept no signer")
	}
	// The signer lives in wintrust's memory until the close above.
	s := *(**providerSigner)(unsafe.Pointer(&sgnr))
	if s.chainLen == 0 || s.chain == nil || s.chain.cert == nil {
		return "", errors.New("the signature has no certificate chain")
	}
	n := windows.CertGetNameString(s.chain.cert, windows.CERT_NAME_SIMPLE_DISPLAY_TYPE, 0, nil, nil, 0)
	if n <= 1 {
		return "", errors.New("the signing certificate has no name")
	}
	name := make([]uint16, n)
	windows.CertGetNameString(s.chain.cert, windows.CERT_NAME_SIMPLE_DISPLAY_TYPE, 0, nil, &name[0], n)
	return windows.UTF16ToString(name), nil
}
