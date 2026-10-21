//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\GabitoTL`
	envKey       = `Environment`

	createNoWindow = 0x08000000
	hwndBroadcast  = 0xffff
	wmSettingChg   = 0x001A
	smtoAbortHung  = 0x0002
)

// New returns an Installer wired to the real Windows adapters. The install is
// per user: nothing here needs administrator rights.
func New() *Installer {
	exe, _ := os.Executable()
	home, _ := os.UserHomeDir()
	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	fs := winFS{}
	return &Installer{
		Env: Env{
			ExePath:           exe,
			DefaultInstallDir: filepath.Join(local, "Programs", "GabitoTL"),
			StartMenuPrograms: knownFolder(windows.FOLDERID_Programs, filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs")),
			DesktopDir:        knownFolder(windows.FOLDERID_Desktop, filepath.Join(home, "Desktop")),
			Protected:         protectedDirs(home, local, roaming),
			DataDir:           filepath.Join(local, "GabitoTL"),
			Today:             func() string { return time.Now().Format("20060102") },
		},
		FS: fs, Path: userPath{}, Links: wshShortcuts{}, Reg: appsRegistry{}, Procs: winProcs{},
	}
}

// protectedDirs are folders an install directory may never be or contain.
func protectedDirs(home, local, roaming string) []string {
	ps := []string{home, local, roaming, os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"),
		os.Getenv("ProgramW6432"), os.Getenv("SystemRoot")}
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_Desktop, windows.FOLDERID_Documents,
		windows.FOLDERID_Downloads, windows.FOLDERID_Programs} {
		ps = append(ps, knownFolder(id, ""))
	}
	return ps
}

// knownFolder resolves a shell folder the way Explorer does, so a Desktop
// redirected to OneDrive or a network share is still found. It falls back to
// the conventional path when the shell cannot answer.
func knownFolder(id *windows.KNOWNFOLDERID, fallback string) string {
	if p, err := windows.KnownFolderPath(id, 0); err == nil && p != "" {
		return p
	}
	return fallback
}

// winFS adds the delayed file removal to the portable file adapter.
type winFS struct{ osFS }

// ScheduleRemoval starts a detached, windowless cleanup process (see
// RemovalCommandLine) that first waits for this process to exit. It outlives this process, which is the point: our own
// exe is locked until we exit. The command line is passed verbatim through
// SysProcAttr.CmdLine so Go does not re-escape the quotes.
func (winFS) ScheduleRemoval(r Removal) error {
	r.WaitPID = os.Getpid()
	line, err := RemovalCommandLine(r)
	if err != nil {
		return err
	}
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       line,
		CreationFlags: createNoWindow | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// userPath edits HKCU\Environment\Path.
type userPath struct{}

func (userPath) Get() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	return v, err
}

// Set keeps REG_EXPAND_SZ (the usual type, and required for %VAR% entries to
// keep working); a missing or plain REG_SZ value keeps its own type.
func (userPath) Set(value string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	_, typ, gerr := k.GetStringValue("Path")
	if gerr == nil && typ == registry.SZ {
		err = k.SetStringValue("Path", value)
	} else {
		err = k.SetExpandStringValue("Path", value)
	}
	if err != nil {
		return err
	}
	broadcastEnvChange()
	return nil
}

// broadcastEnvChange sends WM_SETTINGCHANGE "Environment" so Explorer and new
// terminals pick up the PATH. Failure is harmless (a re-login also works).
func broadcastEnvChange() {
	p, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	var res uintptr
	_, _, _ = proc.Call(hwndBroadcast, wmSettingChg, 0, uintptr(unsafe.Pointer(p)), smtoAbortHung, 5000, uintptr(unsafe.Pointer(&res)))
}

// wshShortcuts creates .lnk files through the WScript.Shell COM object, which
// needs no CGO.
type wshShortcuts struct{}

func (wshShortcuts) Create(lnk string, s Shortcut) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitialize(0); err != nil {
		// S_FALSE (1) means COM was already initialized on this thread.
		var oe *ole.OleError
		if !errors.As(err, &oe) || oe.Code() != 1 {
			return err
		}
	} else {
		defer ole.CoUninitialize()
	}
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return err
	}
	unk, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return err
	}
	defer unk.Release()
	shell, err := unk.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer shell.Release()
	res, err := oleutil.CallMethod(shell, "CreateShortcut", lnk)
	if err != nil {
		return err
	}
	sc := res.ToIDispatch()
	defer sc.Release()
	for name, val := range map[string]string{
		"TargetPath": s.Target, "WorkingDirectory": s.WorkDir,
		"IconLocation": s.Icon, "Description": s.Description,
	} {
		if val == "" {
			continue
		}
		if _, err := oleutil.PutProperty(sc, name, val); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	_, err = oleutil.CallMethod(sc, "Save")
	return err
}

func (wshShortcuts) Remove(lnk string) error {
	if err := os.Remove(lnk); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// appsRegistry manages HKCU\...\Uninstall\GabitoTL.
type appsRegistry struct{}

func (appsRegistry) Register(r Registration) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for name, val := range map[string]string{
		"DisplayName": r.DisplayName, "DisplayVersion": r.DisplayVersion,
		"Publisher": r.Publisher, "DisplayIcon": r.DisplayIcon,
		"InstallLocation": r.InstallLocation, "UninstallString": r.UninstallString,
		"ModifyPath": r.ModifyPath, "InstallDate": r.InstallDate,
	} {
		if err := k.SetStringValue(name, val); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	for name, val := range map[string]uint32{
		"NoRepair": 1, "EstimatedSize": r.EstimatedSizeKB,
	} {
		if err := k.SetDWordValue(name, val); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (appsRegistry) Unregister() error {
	err := registry.DeleteKey(registry.CURRENT_USER, uninstallKey)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func (appsRegistry) Registered() (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("DisplayVersion")
	return v, true
}
