// Package opencodedb reads OpenCode's SQLite database. It is its own package so
// that only the Manager binary links the pure-Go SQLite driver; the gabitotl
// CLI never imports it.
package opencodedb

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // pure Go, CGO-free

	"github.com/winnstorm/gabitotl/internal/projects"
)

// Source lists the `project.worktree` rows of every opencode.db found.
type Source struct{ Env projects.Env }

func (Source) Name() string { return projects.SrcOpenCode }

func (s Source) Discover(ctx context.Context) ([]projects.Candidate, error) {
	var out []projects.Candidate
	for _, p := range projects.OpenCodeDBPaths(s.Env) {
		c, err := readDB(ctx, p)
		if err != nil {
			continue // locked, foreign schema or corrupt: skip silently
		}
		out = append(out, c...)
	}
	return out, nil
}

// dsn opens read-only; OpenCode may be running, so nothing is written or locked
// for longer than a read (busy_timeout bounds the wait).
func dsn(p string) string {
	slash := filepath.ToSlash(p)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash // C:/x -> /C:/x
	}
	u := url.URL{Scheme: "file", Path: slash, RawQuery: "mode=ro&_pragma=busy_timeout(1500)&_pragma=query_only(1)"}
	return u.String()
}

func readDB(ctx context.Context, p string) ([]projects.Candidate, error) {
	db, err := sql.Open("sqlite", dsn(p))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT worktree, time_updated FROM project WHERE worktree IS NOT NULL AND worktree <> '' AND worktree <> '/' LIMIT 2000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []projects.Candidate
	for rows.Next() {
		var wt string
		var ms sql.NullInt64
		if rows.Scan(&wt, &ms) != nil {
			continue
		}
		out = append(out, projects.Candidate{Path: wt, Source: projects.SrcOpenCode, LastSeen: projects.UnixMilli(ms.Int64)})
	}
	return out, rows.Err()
}
