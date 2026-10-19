package gabitotl

import (
	"errors"
	"io/fs"
)

// Global delivery defaults live in the user's own ~/.gabitotl/vcs.md, the same
// format as a project's team vcs.md (docs/DECISIONS.md D56). They are the layer
// under every project: a project file, then the person's own file, win over them.
// The file holds settings only: never a token (credentials live in the OS
// credential store) and never an auth reference.

// GlobalLayout is the layout of the user-level .gabitotl folder under home.
func GlobalLayout(home string) Layout { return NewLayout(home) }

// LoadGlobalVCS returns the user's global defaults over the built-in ones; ok is
// false when the file does not exist (the built-in defaults are returned).
func LoadGlobalVCS(home string) (c VCSConfig, ok bool, err error) {
	c = DefaultVCS("git")
	if home == "" {
		return c, false, nil
	}
	g, err := readSharedVCS(GlobalLayout(home))
	if errors.Is(err, fs.ErrNotExist) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	g.Auth, g.Configured = "", false // global defaults never carry a reference or the project marker
	return g, true, nil
}

// SaveGlobalVCS writes the global defaults. The provider is per project, so it is
// always stored as "git"; auth and the configured marker are never written.
func SaveGlobalVCS(home string, c VCSConfig) error {
	if home == "" {
		return errors.New("no home folder to keep the global delivery defaults in")
	}
	c.Provider, c.Auth, c.Configured = "git", "", false
	return saveConfig(GlobalLayout(home).VCS(), &c, bodyVCS)
}

// SaveGlobalDelivery applies the answers to the global defaults and writes them.
// The provider is ignored (it is per project). It returns the stored defaults.
func SaveGlobalDelivery(home string, a DeliveryAnswers) (VCSConfig, error) {
	a.Provider = ""
	if err := a.Validate(); err != nil {
		return VCSConfig{}, err
	}
	c, _, err := LoadGlobalVCS(home)
	if err != nil {
		return VCSConfig{}, err
	}
	c.applyDelivery(a)
	if err := SaveGlobalVCS(home, c); err != nil {
		return VCSConfig{}, err
	}
	c.Configured = false
	return c, nil
}
