// Package codegraph installs CodeGraph (github.com/colbymchenry/codegraph), the
// code-index tool GabitoTL requires, as a global per-user component of the
// Windows setup.
//
// The release bundle is pinned (version and SHA-256 per asset) in this file:
// nothing is trusted from the network, a hash mismatch fails closed and leaves
// the machine untouched. The layout matches the official install.ps1
// (%LOCALAPPDATA%\codegraph\current, bin\ on the user PATH) so that
// `codegraph upgrade` keeps working. Every side effect goes through a port:
// the downloader, the command runner, the PATH editor (internal/desktop) and
// the file operations (tests use a temp directory).
//
// Only Windows has a real wiring; elsewhere Plan reports Unsupported and
// users install CodeGraph with npm (see Hint).
package codegraph

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/desktop"
)

// Pinned release. Hashes come from the release's SHA256SUMS file:
// https://github.com/colbymchenry/codegraph/releases/download/v1.6.2/SHA256SUMS
const (
	Version = "1.6.2"
	BaseURL = "https://github.com/colbymchenry/codegraph/releases/download/v" + Version + "/"
	// Hint is what a user on another OS does.
	Hint = "npm install -g @colbymchenry/codegraph"
	// MaxDownload caps the archive size (the real one is far smaller).
	MaxDownload = 200 << 20
	// maxUnpacked caps the total uncompressed size against zip bombs.
	maxUnpacked = 1 << 30
)

// Asset is one downloadable bundle.
type Asset struct {
	Name   string
	SHA256 string
}

// Assets by GOOS/GOARCH.
var Assets = map[string]Asset{
	"windows/amd64": {Name: "codegraph-win32-x64.zip", SHA256: "7455e817ee8032666867a1badeb799b8a884c5f4fea9b444e088a02069b6deea"},
	"windows/arm64": {Name: "codegraph-win32-arm64.zip", SHA256: "308f7bf92e7886f4d5f767a4755d703e92d5cae78ac042b8907f1e99db0fc071"},
}

// Step IDs of a plan.
const (
	StepDownload = "download"
	StepVerify   = "verify"
	StepExtract  = "extract"
	StepActivate = "activate"
	StepPath     = "path"
)

// Action says what Plan found to do.
type Action string

const (
	ActionInstall     Action = "install"
	ActionUpgrade     Action = "upgrade"
	ActionSkip        Action = "skip" // already installed at the pinned version or newer
	ActionUnsupported Action = "unsupported"
)

// ErrUnsupported means this OS has no managed install.
var ErrUnsupported = errors.New("codegraph: managed install is only available on Windows; install it with: " + Hint)

// ErrChecksum is returned when the download does not match the pinned hash.
var ErrChecksum = errors.New("codegraph: download does not match the pinned SHA-256; nothing was installed")

// Status is what is installed now.
type Status struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"` // the executable that answered
	OnPath    bool   `json:"onPath"`         // the bin directory is on the user PATH
	Pinned    string `json:"pinned"`
	Supported bool   `json:"supported"`
	// Shadow is another codegraph on PATH next to the managed one (for example
	// an npm install); it is reported, never modified.
	Shadow string `json:"shadow,omitempty"`
}

// Plan is what Install would do.
type Plan struct {
	Action    Action
	Installed string // current version, "" when none
	Pinned    string
	Target    string // the "current" directory
	Steps     []string
}

// Ports are the side effects of the installer.
type Ports struct {
	// Download streams the https URL to dst, failing above max bytes.
	Download Downloader
	// Look finds a binary on PATH.
	Look func(name string) (string, error)
	// Run runs a command and returns its combined output.
	Run func(ctx context.Context, bin string, args ...string) (string, error)
	// Path is the per-user PATH.
	Path desktop.PathEnv
}

// Installer installs and detects CodeGraph.
type Installer struct {
	Ports
	GOOS, GOARCH string
	Root         string // %LOCALAPPDATA%\codegraph
	// Token names the aside files of a replaced install (install.ps1 does the same).
	Token func() string

	rename func(oldp, newp string) error // tests replace it
}

// Default returns the installer for this machine.
func Default() *Installer {
	local := os.Getenv("LOCALAPPDATA")
	root := ""
	if local != "" {
		root = filepath.Join(local, "codegraph")
	}
	return &Installer{
		Ports: Ports{Download: NewHTTPDownloader(), Look: lookPath, Run: runCmd, Path: desktop.New().Path},
		GOOS:  runtime.GOOS, GOARCH: runtime.GOARCH, Root: root,
	}
}

func (in *Installer) asset() (Asset, bool) {
	a, ok := Assets[in.GOOS+"/"+in.GOARCH]
	return a, ok && in.Root != ""
}

// Supported reports whether a managed install exists for this machine.
func (in *Installer) Supported() bool { _, ok := in.asset(); return ok }

// Current is the directory that holds the active install.
func (in *Installer) Current() string { return filepath.Join(in.Root, "current") }

// BinDir is the directory added to the user PATH.
func (in *Installer) BinDir() string { return filepath.Join(in.Current(), "bin") }

// Launcher is the codegraph.cmd of the managed install.
func (in *Installer) Launcher() string { return filepath.Join(in.BinDir(), "codegraph.cmd") }

var (
	semverRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.]+))?`)
	bareRe   = regexp.MustCompile(`^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?)$`)
)

// ParseVersion extracts the version of a `--version` output: the first token
// that is a bare version ("1.6.2", "v1.6.2-rc.1"), else the first x.y.z found.
func ParseVersion(out string) (string, bool) {
	for _, f := range strings.Fields(out) {
		if m := bareRe.FindStringSubmatch(f); m != nil {
			return m[1], true
		}
	}
	m := semverRe.FindString(out)
	return m, m != ""
}

// Compare orders two versions: -1, 0 or 1. A prerelease ("-rc.1") is lower
// than its release.
func Compare(a, b string) int {
	pa, pb := semverRe.FindStringSubmatch(a), semverRe.FindStringSubmatch(b)
	if pa == nil || pb == nil {
		return strings.Compare(a, b)
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case pa[4] == pb[4]:
		return 0
	case pa[4] == "":
		return 1
	case pb[4] == "":
		return -1
	}
	return strings.Compare(pa[4], pb[4])
}

// Status detects an existing install. It always probes both the first
// `codegraph` on PATH and the managed launcher, so an older npm install ahead
// of the managed bin directory cannot hide the managed one. The newest answer
// wins; Shadow names a PATH entry that is not the managed one (never touched).
func (in *Installer) Status(ctx context.Context) Status {
	st := Status{Pinned: Version, Supported: in.Supported()}
	if !st.Supported || in.Look == nil || in.Run == nil {
		return st
	}
	probe := func(bin string) (string, bool) {
		out, err := in.Run(ctx, bin, "--version")
		if err != nil {
			return "", false
		}
		return ParseVersion(out)
	}
	var pathBin, pathVer, mgdVer string
	if p, err := in.Look("codegraph"); err == nil {
		if v, ok := probe(p); ok {
			pathBin, pathVer = p, v
		}
	}
	if _, err := os.Stat(in.Launcher()); err == nil {
		if v, ok := probe(in.Launcher()); ok {
			mgdVer = v
		}
	}
	switch {
	case mgdVer != "" && (pathVer == "" || Compare(mgdVer, pathVer) >= 0):
		st.Installed, st.Version, st.Path = true, mgdVer, in.Launcher()
	case pathVer != "":
		st.Installed, st.Version, st.Path = true, pathVer, pathBin
	}
	if mgdVer != "" && pathBin != "" && !samePath(pathBin, in.Launcher()) {
		st.Shadow = pathBin
	}
	if in.Root != "" && in.Path != nil {
		if p, err := in.Path.Get(); err == nil {
			st.OnPath = desktop.HasPathEntry(p, in.BinDir())
		}
	}
	return st
}

func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

// Plan reports what Install would do. It changes nothing.
func (in *Installer) Plan(ctx context.Context) Plan {
	pl := Plan{Pinned: Version, Target: in.Current()}
	if !in.Supported() {
		pl.Action = ActionUnsupported
		return pl
	}
	st := in.Status(ctx)
	pl.Installed = st.Version
	switch {
	case st.Installed && Compare(st.Version, Version) >= 0:
		pl.Action = ActionSkip
		if !st.OnPath && samePath(st.Path, in.Launcher()) {
			pl.Steps = []string{StepPath}
		}
	case st.Installed:
		pl.Action = ActionUpgrade
		pl.Steps = []string{StepDownload, StepVerify, StepExtract, StepActivate, StepPath}
	default:
		pl.Action = ActionInstall
		pl.Steps = []string{StepDownload, StepVerify, StepExtract, StepActivate, StepPath}
	}
	return pl
}

// Result is the outcome of Install.
type Result struct {
	Action  Action
	Version string
	Path    string
}

// Install runs the plan. step is called before each step (nil is allowed).
func (in *Installer) Install(ctx context.Context, step func(id string)) (Result, error) {
	if step == nil {
		step = func(string) {}
	}
	pl := in.Plan(ctx)
	switch pl.Action {
	case ActionUnsupported:
		return Result{Action: pl.Action}, ErrUnsupported
	case ActionSkip:
		if len(pl.Steps) > 0 { // present but its bin directory is not on PATH yet
			step(StepPath)
			if err := in.ensurePath(); err != nil {
				return Result{Action: pl.Action}, err
			}
		}
		return Result{Action: ActionSkip, Version: pl.Installed, Path: in.Launcher()}, nil
	}
	asset, _ := in.asset()
	if err := os.MkdirAll(in.Root, 0o755); err != nil {
		return Result{}, fmt.Errorf("codegraph: %w", err)
	}
	tmp, err := os.CreateTemp(in.Root, "download-*.zip")
	if err != nil {
		return Result{}, fmt.Errorf("codegraph: %w", err)
	}
	defer os.Remove(tmp.Name())
	step(StepDownload)
	h := sha256.New()
	dlErr := in.Download.Download(ctx, BaseURL+asset.Name, io.MultiWriter(tmp, h), MaxDownload)
	if dlErr != nil {
		tmp.Close()
		return Result{}, fmt.Errorf("codegraph: download: %w", dlErr)
	}
	step(StepVerify)
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), asset.SHA256) {
		tmp.Close()
		return Result{}, ErrChecksum
	}
	step(StepExtract)
	stage := filepath.Join(in.Root, Version)
	if err := os.RemoveAll(stage); err != nil {
		return Result{}, fmt.Errorf("codegraph: %w", err)
	}
	// Extract from the very handle that was hashed: no reopen window.
	size, err := tmp.Seek(0, io.SeekEnd)
	if err == nil {
		err = extractZip(tmp, size, stage)
	}
	tmp.Close()
	if err != nil {
		os.RemoveAll(stage)
		return Result{}, err
	}
	if _, err := os.Stat(filepath.Join(stage, "bin", "codegraph.cmd")); err != nil {
		os.RemoveAll(stage)
		return Result{}, errors.New("codegraph: the bundle has no bin/codegraph.cmd; nothing was installed")
	}
	step(StepActivate)
	if err := in.swap(stage, in.Current()); err != nil {
		return Result{}, err
	}
	step(StepPath)
	if err := in.ensurePath(); err != nil {
		return Result{Action: pl.Action, Version: Version, Path: in.Launcher()}, err
	}
	return Result{Action: pl.Action, Version: Version, Path: in.Launcher()}, nil
}

// ensurePath adds the bin directory to the user PATH once.
func (in *Installer) ensurePath() error {
	cur, err := in.Path.Get()
	if err != nil {
		return fmt.Errorf("codegraph: read PATH: %w", err)
	}
	next, changed := desktop.AddPathEntry(cur, in.BinDir())
	if !changed {
		return nil
	}
	if err := in.Path.Set(next); err != nil {
		return fmt.Errorf("codegraph: update PATH: %w", err)
	}
	return nil
}

func (in *Installer) mv(a, b string) error {
	if in.rename != nil {
		return in.rename(a, b)
	}
	return os.Rename(a, b)
}

func (in *Installer) token() string {
	if in.Token != nil {
		return in.Token()
	}
	var r [4]byte
	_, _ = rand.Read(r[:])
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(os.Getpid()) + "-" + hex.EncodeToString(r[:])
}

// swap makes stage the new current. A running MCP server locks files inside
// current on Windows, so when the directory cannot be renamed the files are
// renamed aside one by one (*.old-<token>) like install.ps1 does, and the new
// ones are moved in; the aside files are removed on a best-effort basis.
func (in *Installer) swap(stage, current string) error {
	in.sweep(current)
	if _, err := os.Stat(current); errors.Is(err, os.ErrNotExist) {
		return in.mv(stage, current)
	}
	tok := ".old-" + in.token()
	aside := current + tok
	os.RemoveAll(aside)
	if err := in.mv(current, aside); err == nil {
		if err := in.mv(stage, current); err != nil {
			_ = in.mv(aside, current) // put the previous install back
			return fmt.Errorf("codegraph: activate: %w", err)
		}
		os.RemoveAll(aside)
		return nil
	}
	return in.mergeAside(stage, current, tok)
}

// sweep removes aside leftovers of earlier upgrades (best effort: a file that
// is still locked stays until the next run).
func (in *Installer) sweep(current string) {
	if ents, err := os.ReadDir(in.Root); err == nil {
		for _, e := range ents {
			if strings.Contains(e.Name(), ".old-") {
				os.RemoveAll(filepath.Join(in.Root, e.Name()))
			}
		}
	}
	_ = filepath.WalkDir(current, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(d.Name(), ".old-") {
			os.Remove(p)
		}
		return nil
	})
}

func (in *Installer) mergeAside(stage, current, tok string) error {
	var moved [][2]string // aside -> original, for rollback
	var created []string  // new files moved in
	rollback := func() {
		for _, c := range created {
			os.Remove(c)
		}
		for i := len(moved) - 1; i >= 0; i-- {
			os.Remove(moved[i][1])
			_ = in.mv(moved[i][0], moved[i][1])
		}
	}
	var files []string
	err := filepath.WalkDir(current, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.Contains(d.Name(), ".old-") {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("codegraph: activate: %w", err)
	}
	for _, f := range files {
		if err := in.mv(f, f+tok); err != nil {
			rollback()
			return fmt.Errorf("codegraph: activate: %w", err)
		}
		moved = append(moved, [2]string{f + tok, f})
	}
	err = filepath.WalkDir(stage, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(stage, p)
		dst := filepath.Join(current, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if err := in.mv(p, dst); err != nil {
			return err
		}
		created = append(created, dst)
		return nil
	})
	if err != nil {
		rollback()
		return fmt.Errorf("codegraph: activate: %w", err)
	}
	os.RemoveAll(stage)
	for _, m := range moved {
		os.Remove(m[0]) // locked files stay behind until the next upgrade
	}
	return nil
}
