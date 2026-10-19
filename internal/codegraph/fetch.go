package codegraph

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Downloader fetches one https URL.
type Downloader interface {
	// Download writes the body to dst and fails when it exceeds max bytes.
	Download(ctx context.Context, url string, dst io.Writer, max int64) error
}

type httpDownloader struct{ client *http.Client }

// NewHTTPDownloader returns the real downloader: https only (also across
// redirects), a total timeout and a size cap.
func NewHTTPDownloader() Downloader { return NewHTTPDownloaderWith(nil) }

// NewHTTPDownloaderWith is NewHTTPDownloader over a given client (tests).
func NewHTTPDownloaderWith(c *http.Client) Downloader {
	if c == nil {
		c = &http.Client{}
	}
	cc := *c
	if cc.Timeout == 0 {
		cc.Timeout = 10 * time.Minute
	}
	prev := cc.CheckRedirect
	cc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to a non-https URL refused: %s", req.URL.Redacted())
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if prev != nil {
			return prev(req, via)
		}
		return nil
	}
	return httpDownloader{client: &cc}
}

func (d httpDownloader) Download(ctx context.Context, url string, dst io.Writer, max int64) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("only https downloads are allowed, got %q", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > max {
		return fmt.Errorf("download is %d bytes, over the %d byte limit", resp.ContentLength, max)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, max+1))
	if err != nil {
		return err
	}
	if n > max {
		return fmt.Errorf("download is over the %d byte limit", max)
	}
	return nil
}

// verifySHA256 compares the file with the pinned hash. It fails closed.
func verifySHA256(file, want string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want) {
		return ErrChecksum
	}
	return nil
}

// extractZip unpacks the archive into dst, dropping its single top-level
// directory. Entries that would land outside dst (absolute, drive letter, ..),
// symlinks and archives that unpack to more than maxUnpacked are refused.
func extractZip(archive, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("codegraph: bad archive: %w", err)
	}
	defer zr.Close()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	var total int64
	top := ""
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
			return fmt.Errorf("codegraph: unsafe path in archive: %q", f.Name)
		}
		parts := strings.Split(path.Clean(name), "/")
		for _, p := range parts {
			if p == ".." {
				return fmt.Errorf("codegraph: unsafe path in archive: %q", f.Name)
			}
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("codegraph: symlink in archive refused: %q", f.Name)
		}
		if top == "" {
			top = parts[0]
		} else if parts[0] != top {
			return fmt.Errorf("codegraph: archive has more than one top-level entry (%q, %q)", top, parts[0])
		}
		rel := strings.Join(parts[1:], "/")
		if rel == "" || rel == "." {
			continue
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if r, err := filepath.Rel(dst, target); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return fmt.Errorf("codegraph: unsafe path in archive: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > maxUnpacked {
			return errors.New("codegraph: archive unpacks to too much data")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != int64(f.UncompressedSize64) {
		err = fmt.Errorf("codegraph: %q does not match its declared size", f.Name)
	}
	return err
}

func lookPath(name string) (string, error) { return exec.LookPath(name) }

func runCmd(ctx context.Context, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	return string(out), err
}
