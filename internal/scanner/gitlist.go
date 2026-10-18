package scanner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// gitFiles lists the files of the work tree under root the way Git sees them:
// tracked plus untracked-but-not-ignored, so .gitignore is honoured and huge
// monorepos need no directory walk. ok is false outside a repository (or
// without git); callers then fall back to the walk.
func gitFiles(ctx context.Context, root string) ([]string, bool) {
	out, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, false
	}
	return splitNUL(out), true
}

func splitNUL(b []byte) []string {
	var out []string
	for _, p := range bytes.Split(b, []byte{0}) {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out
}

// runList fills the walk from a git listing. The first w.max files are read
// in detail; the rest are only counted per top-level directory and extension,
// so a huge repo degrades to a summary and is flagged truncated.
func (w *walk) runList(files []string) {
	for _, rel := range files {
		if rel == ".gabitotl" || strings.HasPrefix(rel, ".gabitotl/") || strings.HasPrefix(rel, ".git/") {
			continue
		}
		if strings.HasPrefix(rel, ".github/workflows/") {
			w.flags["github-actions"] = true
		}
		if w.detailed >= w.max {
			w.truncated = true
			w.visited++
			w.countOnly(rel)
			continue
		}
		fi, err := os.Lstat(filepath.Join(w.root, filepath.FromSlash(rel)))
		if err != nil || !fi.Mode().IsRegular() {
			continue // deleted in the work tree, a submodule or a symlink
		}
		w.visited++
		w.detailed++
		w.record(rel, path.Base(rel), fi.Size())
	}
}

func (w *walk) countOnly(rel string) {
	ext := strings.ToLower(path.Ext(rel))
	if ext == "" {
		ext = "(none)"
	}
	w.ext[ext]++
	if i := strings.IndexByte(rel, '/'); i < 0 {
		w.topFiles++
	} else {
		w.top[rel[:i]]++
	}
}

// gitState returns HEAD and a fingerprint of the untracked-not-ignored set.
// ok is false when root is not a usable Git work tree.
func gitState(ctx context.Context, root string) (head, untracked string, ok bool) {
	if out, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output(); err == nil {
		head = strings.TrimSpace(string(out))
	}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--others", "--exclude-standard").Output()
	if err != nil {
		return "", "", false
	}
	sum := sha256.Sum256(out)
	return head, hex.EncodeToString(sum[:]), true
}
