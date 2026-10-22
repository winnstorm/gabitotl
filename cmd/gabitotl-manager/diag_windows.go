//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/winnstorm/gabitotl/internal/applog"
)

// openLog opens %LOCALAPPDATA%\GabitoTL\logs\manager.log. A nil logger (when the
// folder cannot be created) is valid: the app still runs, just without a file.
func openLog() *applog.Logger {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		if c, err := os.UserCacheDir(); err == nil {
			local = c
		}
	}
	l, err := applog.Open(applog.PathIn(local), applog.DefaultMax)
	if err != nil {
		return nil
	}
	return l
}

// messageBox shows a native error dialog; the GUI exe has no console, so this is
// how a failure that happens before the window exists reaches the person.
func messageBox(title, text string) {
	t, err1 := windows.UTF16PtrFromString(title)
	m, err2 := windows.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return
	}
	const mbOK, mbIconError = 0x0, 0x10
	_, _ = windows.MessageBox(0, m, t, mbOK|mbIconError)
}

// webView2Version reads the installed WebView2 runtime version ("" when none).
func webView2Version() string {
	const guid = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	keys := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + guid},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\EdgeUpdate\Clients\` + guid},
		{registry.CURRENT_USER, `Software\Microsoft\EdgeUpdate\Clients\` + guid},
	}
	for _, k := range keys {
		h, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := h.GetStringValue("pv")
		h.Close()
		if err == nil && v != "" && v != "0.0.0.0" {
			return v
		}
	}
	return ""
}

// wailsLog routes Wails' own log lines (including recovered panics in bound
// methods) into the app log.
type wailsLog struct{ l *applog.Logger }

func (w wailsLog) Print(m string)   { w.l.Infof("wails: %s", m) }
func (w wailsLog) Trace(m string)   {}
func (w wailsLog) Debug(m string)   {}
func (w wailsLog) Info(m string)    { w.l.Infof("wails: %s", m) }
func (w wailsLog) Warning(m string) { w.l.Infof("wails warning: %s", m) }
func (w wailsLog) Error(m string)   { w.l.Errorf("wails: %s", m) }
func (w wailsLog) Fatal(m string)   { w.l.Errorf("wails fatal: %s", m) }

// localDir is %LOCALAPPDATA% (the cache dir's parent fallback).
func localDir() string {
	if l := os.Getenv("LOCALAPPDATA"); l != "" {
		return l
	}
	if c, err := os.UserCacheDir(); err == nil {
		return c
	}
	return filepath.Join(os.TempDir())
}

func goVersion() string { return runtime.Version() }
