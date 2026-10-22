// Package engraminstall installs Engram (github.com/Gentleman-Programming/engram),
// the local memory store, when a project chooses Engram and the binary is
// missing.
//
// Like internal/codegraph the release is pinned in this file (version and
// SHA-256 per asset, read from the official release's checksums.txt) and
// nothing is trusted from the network: a hash mismatch fails closed and leaves
// the machine untouched. An Engram that is already on PATH (any version) or
// already installed by GabitoTL is never replaced.
//
// The binary goes to a per-user directory (no admin): %LOCALAPPDATA%\Programs\engram
// on Windows, which is added to the user PATH, and ~/.local/bin on Linux and
// macOS, where the user PATH is only reported, never edited. Every side effect
// goes through a port so tests run on a temp directory.
package engraminstall

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/codegraph"
	"github.com/winnstorm/gabitotl/internal/desktop"
)

// Pinned release. Hashes come from the release's checksums file:
// https://github.com/Gentleman-Programming/engram/releases/download/v3.5.2/checksums.txt
const (
	Version = "3.5.2"
	BaseURL = "https://github.com/Gentleman-Programming/engram/releases/download/v" + Version + "/"
	// SourceURL is the official repository, shown as information only.
	SourceURL = "https://github.com/Gentleman-Programming/engram"
	// MaxDownload caps the archive size (the real ones are about 8 MB).
	MaxDownload = 100 << 20
	// maxBinary caps the unpacked executable (the real one is about 22 MB).
	maxBinary = 300 << 20
)

// Asset is one downloadable archive.
type Asset struct {
	Name   string
	SHA256 string
}

// Assets by GOOS/GOARCH.
var Assets = map[string]Asset{
	"windows/amd64": {Name: "engram_3.5.2_windows_amd64.zip", SHA256: "ba56a36d37b2da85fcb1581bbdbed9ae1ee690e642aab8dbe1e18209bf264afe"},
	"windows/arm64": {Name: "engram_3.5.2_windows_arm64.zip", SHA256: "df5acc5dd6ee407d484074e570c77a79567c0b8dccf55e25ae25a644c1e7db5f"},
	"linux/amd64":   {Name: "engram_3.5.2_linux_amd64.tar.gz", SHA256: "106a43ca8f43a27c2c260a6787151c3be6d91c5ed3d5e454e6140527adf92e01"},
	"linux/arm64":   {Name: "engram_3.5.2_linux_arm64.tar.gz", SHA256: "b0ba49cf5a7d5af436c9744a9a0d638b3f9ebb2a7321c8736d30e96305c2993c"},
	"darwin/amd64":  {Name: "engram_3.5.2_darwin_amd64.tar.gz", SHA256: "aea28ebf461a4ea86c52b793c29311539d7123875354ed7411eae982fc82ec87"},
	"darwin/arm64":  {Name: "engram_3.5.2_darwin_arm64.tar.gz", SHA256: "43d398b2fe3327a4a6436c26b2a17d3285ba80bf2f11a22d0796ea2a7fd2b78c"},
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
	ActionSkip        Action = "skip" // an Engram is already there
	ActionUnsupported Action = "unsupported"
)

// ErrUnsupported means there is no pinned release for this OS or architecture.
var ErrUnsupported = errors.New("engram: no managed install for this system; get it from " + SourceURL)

// ErrChecksum is returned when the download does not match the pinned hash.
var ErrChecksum = errors.New("engram: download does not match the pinned SHA-256; nothing was installed")

// Status is what is installed now.
type Status struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"` // the executable that answered
	Managed   bool   `json:"managed"`        // it is the copy GabitoTL installed
	OnPath    bool   `json:"onPath"`         // the install directory is on PATH
	Dir       string `json:"dir,omitempty"`  // where GabitoTL installs it
	Pinned    string `json:"pinned"`         // the version GabitoTL installs
	Supported bool   `json:"supported"`      // a pinned release exists for this system
	Source    string `json:"source"`         // official repository (information)
}

// Plan is what Install would do.
type Plan struct {
	Action    Action
	Installed string // current version, "" when none or unknown
	Pinned    string
	Target    string // the executable that would be written
	Steps     []string
}

// Ports are the side effects of the installer.
type Ports struct {
	// Download streams the https URL to dst, failing above max bytes.
	Download codegraph.Downloader
	// Look finds a binary on PATH.
	Look func(name string) (string, error)
	// Run runs a command and returns its combined output.
	Run func(ctx context.Context, bin string, args ...string) (string, error)
	// Path is the per-user PATH (Windows); nil leaves the user PATH alone.
	Path desktop.PathEnv
	// Getenv reads the process environment (PATH).
	Getenv func(string) string
	// SetProcessPath makes dir visible to this process, so the installer run
	// that follows finds the new binary without a restart.
	SetProcessPath func(dir string)
}

// Installer installs and detects Engram.
type Installer struct {
	Ports
	GOOS, GOARCH string
	Dir          string // the install directory
	// BaseURL and Table replace the pinned release (tests only).
	BaseURL string
	Table   map[string]Asset
	// Token names the staging and aside files.
	Token func() string

	rename func(oldp, newp string) error // tests replace it
}

// Default returns the installer for this machine.
func Default() *Installer {
	in := &Installer{
		Ports: Ports{
			Download: codegraph.NewHTTPDownloader(), Look: exec.LookPath, Run: runCmd,
			Getenv: os.Getenv, SetProcessPath: prependProcessPath,
		},
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	}
	if in.GOOS == "windows" {
		in.Path = desktop.New().Path
	}
	in.Dir = DefaultDir(in.GOOS, os.Getenv, os.UserHomeDir)
	return in
}

// DefaultDir is the install directory for goos ("" when it cannot be resolved).
func DefaultDir(goos string, getenv func(string) string, home func() (string, error)) string {
	if goos == "windows" {
		if l := getenv("LOCALAPPDATA"); l != "" {
			return filepath.Join(l, "Programs", "engram")
		}
		return ""
	}
	h, err := home()
	if err != nil || h == "" {
		return ""
	}
	return filepath.Join(h, ".local", "bin")
}

func prependProcessPath(dir string) {
	cur := os.Getenv("PATH")
	if pathHas(cur, dir) {
		return
	}
	if cur == "" {
		_ = os.Setenv("PATH", dir)
		return
	}
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+cur)
}

func pathHas(list, dir string) bool {
	for _, e := range filepath.SplitList(list) {
		if e != "" && strings.EqualFold(filepath.Clean(e), filepath.Clean(dir)) {
			return true
		}
	}
	return false
}

func (in *Installer) asset() (Asset, bool) {
	t := in.Table
	if t == nil {
		t = Assets
	}
	a, ok := t[in.GOOS+"/"+in.GOARCH]
	return a, ok && in.Dir != ""
}

// Supported reports whether a managed install exists for this machine.
func (in *Installer) Supported() bool { _, ok := in.asset(); return ok }

// BinaryName is engram or engram.exe.
func (in *Installer) BinaryName() string {
	if in.GOOS == "windows" {
		return "engram.exe"
	}
	return "engram"
}

// Target is the executable GabitoTL installs.
func (in *Installer) Target() string { return filepath.Join(in.Dir, in.BinaryName()) }

// Hint is how a user on a system without a pinned release gets Engram.
func Hint() string { return SourceURL }

func (in *Installer) baseURL() string {
	if in.BaseURL != "" {
		return in.BaseURL
	}
	return BaseURL
}

func (in *Installer) getenv(k string) string {
	if in.Getenv != nil {
		return in.Getenv(k)
	}
	return os.Getenv(k)
}

// onPath tells whether the install directory is on the PATH new programs see.
func (in *Installer) onPath() bool {
	if in.Dir == "" {
		return false
	}
	if pathHas(in.getenv("PATH"), in.Dir) {
		return true
	}
	if in.Path != nil {
		if p, err := in.Path.Get(); err == nil {
			return desktop.HasPathEntry(p, in.Dir)
		}
	}
	return false
}

func (in *Installer) probe(ctx context.Context, bin string) string {
	if in.Run == nil {
		return ""
	}
	out, err := in.Run(ctx, bin, "version")
	if err != nil {
		return ""
	}
	v, _ := codegraph.ParseVersion(out)
	return v
}

// Status detects an existing Engram: first the one on PATH (any version, never
// replaced), then the copy GabitoTL installed that PATH does not reach yet.
func (in *Installer) Status(ctx context.Context) Status {
	st := Status{Pinned: Version, Supported: in.Supported(), Dir: in.Dir, Source: SourceURL}
	if in.Look != nil {
		if p, err := in.Look("engram"); err == nil {
			st.Installed, st.Path, st.Version = true, p, in.probe(ctx, p)
			st.Managed = in.Dir != "" && samePath(filepath.Dir(p), in.Dir)
		}
	}
	if !st.Installed && in.Dir != "" {
		if fi, err := os.Stat(in.Target()); err == nil && fi.Mode().IsRegular() {
			st.Installed, st.Managed, st.Path = true, true, in.Target()
			st.Version = in.probe(ctx, st.Path)
		}
	}
	st.OnPath = in.onPath()
	return st
}

func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

// Plan reports what Install would do. It changes nothing.
func (in *Installer) Plan(ctx context.Context) Plan {
	pl := Plan{Pinned: Version, Target: in.Target()}
	if !in.Supported() {
		pl.Action = ActionUnsupported
		return pl
	}
	st := in.Status(ctx)
	pl.Installed = st.Version
	switch {
	case st.Installed:
		pl.Action = ActionSkip
		if st.Managed && !st.OnPath && in.Path != nil {
			pl.Steps = []string{StepPath} // ours, but its directory is not on the user PATH
		}
	default:
		pl.Action = ActionInstall
		pl.Steps = []string{StepDownload, StepVerify, StepExtract, StepActivate}
		if in.Path != nil {
			pl.Steps = append(pl.Steps, StepPath)
		}
	}
	return pl
}

// Result is the outcome of Install.
type Result struct {
	Action  Action
	Version string
	Path    string
	// NotOnPath is true when the install directory is not on PATH and GabitoTL
	// did not add it (Linux and macOS): the user is told to.
	NotOnPath bool
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
		st := in.Status(ctx)
		res := Result{Action: ActionSkip, Version: pl.Installed, Path: st.Path}
		if len(pl.Steps) > 0 {
			step(StepPath)
			if err := in.ensurePath(); err != nil {
				return res, err
			}
		}
		return res, nil
	}
	asset, _ := in.asset()
	if err := os.MkdirAll(in.Dir, 0o755); err != nil {
		return Result{}, fmt.Errorf("engram: %w", err)
	}
	dl, err := os.CreateTemp("", "engram-download-*")
	if err != nil {
		return Result{}, fmt.Errorf("engram: %w", err)
	}
	defer os.Remove(dl.Name())
	defer dl.Close()

	step(StepDownload)
	h := sha256.New()
	if err := in.Download.Download(ctx, in.baseURL()+asset.Name, io.MultiWriter(dl, h), MaxDownload); err != nil {
		return Result{}, fmt.Errorf("engram: download: %w", err)
	}
	step(StepVerify)
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), asset.SHA256) {
		return Result{}, ErrChecksum
	}
	step(StepExtract)
	size, err := dl.Seek(0, io.SeekEnd)
	if err != nil {
		return Result{}, fmt.Errorf("engram: %w", err)
	}
	tok := in.token()
	stage := filepath.Join(in.Dir, "."+in.BinaryName()+".new-"+tok)
	defer os.Remove(stage)
	if err := extractBinary(dl, size, strings.HasSuffix(asset.Name, ".zip"), in.BinaryName(), stage); err != nil {
		return Result{}, err
	}
	step(StepActivate)
	if err := in.swap(stage, in.Target(), tok); err != nil {
		return Result{}, err
	}
	if in.SetProcessPath != nil {
		in.SetProcessPath(in.Dir)
	}
	res := Result{Action: ActionInstall, Version: Version, Path: in.Target()}
	if in.Path != nil {
		step(StepPath)
		if err := in.ensurePath(); err != nil {
			return res, err
		}
	} else {
		res.NotOnPath = !in.onPath()
	}
	return res, nil
}

// ensurePath adds the install directory to the user PATH once.
func (in *Installer) ensurePath() error {
	if in.Path == nil {
		return nil
	}
	cur, err := in.Path.Get()
	if err != nil {
		return fmt.Errorf("engram: read PATH: %w", err)
	}
	next, changed := desktop.AddPathEntry(cur, in.Dir)
	if changed {
		if err := in.Path.Set(next); err != nil {
			return fmt.Errorf("engram: update PATH: %w", err)
		}
	}
	if in.SetProcessPath != nil {
		in.SetProcessPath(in.Dir)
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

// swap moves the staged binary onto target. An existing target is renamed
// aside first (a running executable can be renamed on Windows, not
// overwritten) and put back if the move fails.
func (in *Installer) swap(stage, target, tok string) error {
	aside := ""
	if _, err := os.Stat(target); err == nil {
		aside = target + ".old-" + tok
		os.Remove(aside)
		if err := in.mv(target, aside); err != nil {
			return fmt.Errorf("engram: activate: %w", err)
		}
	}
	if err := in.mv(stage, target); err != nil {
		if aside != "" {
			_ = in.mv(aside, target) // put the previous install back
		}
		return fmt.Errorf("engram: activate: %w", err)
	}
	if aside != "" {
		os.Remove(aside) // best effort: a locked file stays until the next run
	}
	return nil
}

// extractBinary writes the single executable named want from the archive to
// dst (0755). Every entry name is validated first, so an archive with an
// absolute path, a drive letter, ".." or a symlink is refused as a whole; only
// the entry whose cleaned name is exactly want is read, so nothing else can
// land on disk.
func extractBinary(f *os.File, size int64, isZip bool, want, dst string) error {
	var err error
	if isZip {
		err = fromZip(f, size, want, dst)
	} else {
		if _, err = f.Seek(0, io.SeekStart); err == nil {
			err = fromTarGz(f, want, dst)
		}
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

func checkName(name string) error {
	n := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(n, "/") || strings.Contains(n, ":") {
		return fmt.Errorf("engram: unsafe path in archive: %q", name)
	}
	for _, p := range strings.Split(path.Clean(n), "/") {
		if p == ".." || badSegment(p) {
			return fmt.Errorf("engram: unsafe path in archive: %q", name)
		}
	}
	return nil
}

// badSegment refuses names Windows treats specially: reserved device names
// (with any extension) and trailing dots or spaces.
func badSegment(seg string) bool {
	if seg != strings.TrimRight(seg, ". ") && seg != "." {
		return true
	}
	base := strings.ToLower(seg)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.TrimRight(base, " ") {
	case "con", "prn", "aux", "nul", "conin$", "conout$":
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9'
}

func cleanName(name string) string { return path.Clean(strings.ReplaceAll(name, "\\", "/")) }

func fromZip(r io.ReaderAt, size int64, want, dst string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("engram: bad archive: %w", err)
	}
	var hit *zip.File
	for _, f := range zr.File {
		if err := checkName(f.Name); err != nil {
			return err
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("engram: symlink in archive refused: %q", f.Name)
		}
		if cleanName(f.Name) == want && !f.FileInfo().IsDir() {
			if hit != nil {
				return fmt.Errorf("engram: duplicate %s in archive", want)
			}
			hit = f
		}
	}
	if hit == nil {
		return fmt.Errorf("engram: the archive has no %s; nothing was installed", want)
	}
	if hit.UncompressedSize64 > maxBinary {
		return errors.New("engram: the executable is larger than expected")
	}
	rc, err := hit.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return writeBinary(rc, int64(hit.UncompressedSize64), dst)
}

func fromTarGz(r io.Reader, want, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("engram: bad archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("engram: bad archive: %w", err)
		}
		if err := checkName(h.Name); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("engram: link in archive refused: %q", h.Name)
		case tar.TypeReg:
			if cleanName(h.Name) != want {
				continue
			}
			if found {
				return fmt.Errorf("engram: duplicate %s in archive", want)
			}
			if h.Size > maxBinary {
				return errors.New("engram: the executable is larger than expected")
			}
			if err := writeBinary(tr, h.Size, dst); err != nil {
				return err
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("engram: the archive has no %s; nothing was installed", want)
	}
	return nil
}

func writeBinary(src io.Reader, size int64, dst string) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(src, size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && (n != size || n == 0) {
		err = errors.New("engram: the executable does not match its declared size")
	}
	return err
}

func runCmd(ctx context.Context, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}
