//go:build windows

package credstore

import (
	"errors"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// New returns the credential store of the user: the Windows Credential Manager
// (DPAPI, per user). home is unused on Windows.
func New(string) Store { return manager{} }

const (
	credTypeGeneric         = 1 // CRED_TYPE_GENERIC
	credPersistLocalMachine = 2 // CRED_PERSIST_LOCAL_MACHINE
	userName                = "gabitotl"
)

var (
	advapi32      = windows.NewLazySystemDLL("advapi32.dll")
	procCredWrite = advapi32.NewProc("CredWriteW")
	procCredRead  = advapi32.NewProc("CredReadW")
	procCredDel   = advapi32.NewProc("CredDeleteW")
	procCredFree  = advapi32.NewProc("CredFree")
)

// credential mirrors CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type manager struct{}

func (manager) Get(host string) (string, bool, error) {
	if err := check(host, "", false); err != nil {
		return "", false, err
	}
	target, err := windows.UTF16PtrFromString(Target(host))
	if err != nil {
		return "", false, err
	}
	var out *credential
	r, _, e := procCredRead.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		if errors.Is(e, syscall.Errno(1168)) { // ERROR_NOT_FOUND
			return "", false, nil
		}
		return "", false, e
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(out)))
	blob := unsafe.Slice(out.CredentialBlob, out.CredentialBlobSize)
	return strings.TrimSpace(string(blob)), true, nil
}

func (manager) Set(host, token string) error {
	if err := check(host, token, true); err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(Target(host))
	if err != nil {
		return err
	}
	user, _ := windows.UTF16PtrFromString(userName)
	blob := []byte(strings.TrimSpace(token))
	if len(blob) > MaxTokenBytes {
		return ErrTokenTooLong
	}
	c := credential{
		Type: credTypeGeneric, TargetName: target, UserName: user, Persist: credPersistLocalMachine,
		CredentialBlobSize: uint32(len(blob)), CredentialBlob: &blob[0],
	}
	r, _, e := procCredWrite.Call(uintptr(unsafe.Pointer(&c)), 0)
	if r == 0 {
		return e
	}
	return nil
}

func (manager) Delete(host string) error {
	if err := check(host, "", false); err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(Target(host))
	if err != nil {
		return err
	}
	r, _, e := procCredDel.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r == 0 && !errors.Is(e, syscall.Errno(1168)) {
		return e
	}
	return nil
}
