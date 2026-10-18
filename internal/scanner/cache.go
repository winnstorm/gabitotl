package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// cacheVersion invalidates old caches when the map shape or scan rules change.
const cacheVersion = 1

// cache is the stored map plus what it was built from.
type cache struct {
	Version   int         `json:"version"`
	Root      string      `json:"root"`
	MaxFiles  int         `json:"max_files"`
	Head      string      `json:"head"`
	Untracked string      `json:"untracked"` // fingerprint of the untracked-not-ignored set
	Map       *ProjectMap `json:"map"`
}

// Refresh returns the project map, reusing the one stored at cachePath while
// HEAD and the untracked-file fingerprint are unchanged. The map is an
// aggregate (counts, modules, flags), so there is no per-file entry to patch:
// when either changed it is rebuilt in full, which is cheap because the file
// list comes from `git ls-files`. force (or an empty cachePath, a non-Git
// root or an unreadable cache) skips the reuse. rebuilt reports whether a
// scan ran. Tracked files edited without a commit do not invalidate the map.
func Refresh(ctx context.Context, opt Options, cachePath string, force bool) (pm *ProjectMap, rebuilt bool, err error) {
	root, err := filepath.Abs(opt.Root)
	if err != nil {
		return nil, false, err
	}
	head, untracked, ok := gitState(ctx, root)
	if ok && cachePath != "" && !force {
		if c := loadCache(cachePath); c != nil && c.Version == cacheVersion && c.Root == root &&
			c.MaxFiles == opt.maxFiles() && c.Head == head && c.Untracked == untracked {
			return c.Map, false, nil
		}
	}
	pm, err = Scan(ctx, opt)
	if err != nil {
		return nil, false, err
	}
	if ok && cachePath != "" {
		saveCache(cachePath, &cache{Version: cacheVersion, Root: root, MaxFiles: opt.maxFiles(), Head: head, Untracked: untracked, Map: pm})
	}
	return pm, true, nil
}

func loadCache(p string) *cache {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var c cache
	if json.Unmarshal(b, &c) != nil || c.Map == nil {
		return nil
	}
	return &c
}

// saveCache is best effort: a failed write only costs the next scan a rebuild.
func saveCache(p string, c *cache) {
	b, err := json.Marshal(c)
	if err != nil || os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}
