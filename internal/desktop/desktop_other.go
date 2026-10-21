//go:build !windows

package desktop

import "time"

type unsupported struct{}

func (unsupported) CopyFile(string, string) (int64, error)   { return 0, ErrUnsupported }
func (unsupported) WriteFile(string, []byte) (int64, error)  { return 0, ErrUnsupported }
func (unsupported) Exists(string) bool                       { return false }
func (unsupported) RemoveAll(string) error                   { return ErrUnsupported }
func (unsupported) ScheduleRemoval(Removal) error            { return ErrUnsupported }
func (unsupported) Get() (string, error)                     { return "", ErrUnsupported }
func (unsupported) Set(string) error                         { return ErrUnsupported }
func (unsupported) Create(string, Shortcut) error            { return ErrUnsupported }
func (unsupported) Remove(string) error                      { return ErrUnsupported }
func (unsupported) Register(Registration) error              { return ErrUnsupported }
func (unsupported) Unregister() error                        { return ErrUnsupported }
func (unsupported) Registered() (string, bool)               { return "", false }
func (unsupported) Running(string, []string) ([]Proc, error) { return nil, nil }
func (unsupported) Stop(Proc, time.Duration) error           { return nil }

// New returns an Installer whose every operation fails with ErrUnsupported.
func New() *Installer {
	u := unsupported{}
	return &Installer{FS: u, Path: u, Links: u, Reg: u, Procs: u}
}
