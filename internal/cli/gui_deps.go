package cli

import (
	"context"
	"github.com/winnstorm/gabitotl/internal/projects"
	"io"

	"github.com/winnstorm/gabitotl/internal/credstore"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/guard"
	"github.com/winnstorm/gabitotl/internal/gui"
	"github.com/winnstorm/gabitotl/internal/vcs"
	"github.com/winnstorm/gabitotl/internal/vcshosts"
	"github.com/winnstorm/gabitotl/internal/version"
)

// GUIDeps is the composition root of the desktop app: the same installer
// environment, platform login check and guard code the CLI commands use, handed
// to the headless backend (internal/gui) without it importing this package.
// cliPath resolves, when the hooks are installed, the gabitotl executable they
// call ("" means the running executable); gui passes the freshly installed one.
func GUIDeps(cliPath func() string) gui.Deps {
	return gui.Deps{
		Env:          installerEnvFn,
		VCSAuth:      guiAuth,
		GuardHistory: guiGuardHistory,
		GuardHooks: func(ctx context.Context, root string, install bool, cli string) error {
			if cli == "" && cliPath != nil {
				cli = cliPath()
			}
			if install && cli == "" {
				return gui.ErrCLINotInstalled
			}
			return guiGuardHooks(ctx, root, install, cli)
		},
		Credentials: func(home string) credstore.Store { return credStoreFn(home) },
		VerifyToken: guiVerifyToken,
		Servers:     func(home string) vcshosts.Registry { return serversFn(home) },
		Projects: func(ctx context.Context) ([]projects.Project, error) {
			e := projects.OSEnv()
			return projects.Scan(ctx, projects.Defaults(e), projects.DefaultOptions(e.Home)), nil
		},
		Version: version.Version,
	}
}

// guiVerifyToken checks a token against the platform API with no project.
func guiVerifyToken(ctx context.Context, platform, host, baseURL, token string) (bool, string, string, error) {
	st, err := vcs.VerifyToken(ctx, platform, host, token, vcs.VerifyOptions{HTTP: vcsHTTPFn(), APIBase: vcsAPIFn(), BaseURL: baseURL})
	if err != nil {
		return false, "", "", err
	}
	return st.OK(), st.Detail, st.User, nil
}

// guiAuth is `vcs login --check` as data.
func guiAuth(ctx context.Context, root string) (gui.SignIn, error) {
	cfg, err := loadVCSOrDefault(gabitotl.NewLayout(root), gabitotl.ActiveUserID())
	if err != nil {
		return gui.SignIn{}, err
	}
	st, known := authOf(ctx, root, cfg)
	out := gui.SignIn{Provider: st.Provider, Known: known, OK: st.OK(), Command: st.Command, URL: st.InstallURL, Detail: st.Detail}
	switch st.Status {
	case vcs.AuthLoggedIn:
		out.Status = "logged-in"
	case vcs.AuthNotLoggedIn:
		out.Status = "not-logged-in"
	case vcs.AuthToolMissing:
		out.Status = "tool-missing"
	case vcs.AuthUnsupported:
		out.Status = "unsupported"
	default:
		out.Status = "unknown"
	}
	return out, nil
}

// guiGuardHistory is `guard scan --history` as data (read-only).
func guiGuardHistory(ctx context.Context, root string) (gui.GuardScan, error) {
	s, _, err := guardSettings(root)
	if err != nil {
		return gui.GuardScan{}, err
	}
	found, err := guard.ScanAll(ctx, gitRunner(), root, s)
	if err != nil {
		return gui.GuardScan{}, err
	}
	res, err := guard.ScanHistoryIncremental(ctx, gitRunner(), root, s, historyStore(root), false)
	if err != nil {
		return gui.GuardScan{}, err
	}
	out := gui.GuardScan{Mode: s.Mode, Findings: len(found), History: []gui.GuardFinding{}}
	for _, h := range res.Findings {
		out.History = append(out.History, gui.GuardFinding{Commit: h.Commit, Where: h.Where, Text: h.Text})
	}
	return out, nil
}

// guiGuardHooks is `guard install-hooks` / `uninstall-hooks`.
func guiGuardHooks(ctx context.Context, root string, install bool, bin string) error {
	dir, err := guard.HooksDir(ctx, gitRunner(), root)
	if err != nil {
		return err
	}
	if !install {
		if _, err := guard.UninstallHooks(dir); err != nil {
			return err
		}
		rememberHooks(root, gabitotl.HooksDeclined)
		return nil
	}
	if bin == "" {
		return gui.ErrCLINotInstalled
	}
	return installHooksBin(io.Discard, root, dir, bin)
}
