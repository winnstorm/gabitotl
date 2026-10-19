package gabitotl

import (
	"os"
	"path/filepath"

	"github.com/winnstorm/gabitotl/internal/persona"
)

// The chat concision mode (docs/DECISIONS.md D38) is personal: it never changes
// what a teammate sees. It lives in
//
//   - .gabitotl/local/users/<id>/persona.md (git-ignored) inside a project, or
//   - <home>/.gabitotl/persona.md outside one.
//
// Both carry the front matter key `mode`. Reading goes project file, home file,
// normal; a missing, unreadable or invalid file reads as the next step.

type personaFront struct {
	Mode string `yaml:"mode,omitempty"`
	// Lang is the persona language (docs/DECISIONS.md D54). It only lives in the
	// home file, next to the global persona it selects.
	Lang string `yaml:"lang,omitempty"`
}

const bodyPersonaMode = `
# Chat mode

How short Gabito's chat replies are: normal, lite or full. Personal setting, never
committed. It only affects chat, never code, commits, PRs, docs or memory.
`

// HomePersonaPath is the persona mode file outside a project.
func HomePersonaPath(home string) string {
	return filepath.Join(home, ".gabitotl", "persona.md")
}

func readPersonaFile(path string) (persona.Mode, bool) {
	d, err := ReadDoc(path)
	if err != nil || !d.HasFront {
		return "", false
	}
	var f personaFront
	if d.Decode(&f) != nil {
		return "", false
	}
	m, err := persona.ParseMode(f.Mode)
	return m, err == nil
}

// LoadPersonaMode returns the active chat mode of user: the project per-person
// file, then the home file, then normal. It never fails; l may be the zero
// Layout's root ("") when there is no project.
func LoadPersonaMode(l Layout, user, home string) persona.Mode {
	if l.Root != "" && ValidID(user) {
		if p, err := l.LocalPersona(user); err == nil {
			if m, ok := readPersonaFile(p); ok {
				return m
			}
		}
	}
	if home != "" {
		if m, ok := readPersonaFile(HomePersonaPath(home)); ok {
			return m
		}
	}
	return persona.ModeNormal
}

// SavePersonaMode stores m for user and returns the file written: the project
// per-person file when root holds a .gabitotl and user is a valid id, else the
// home file.
func SavePersonaMode(root, user, home string, m persona.Mode) (string, error) {
	if _, err := persona.ParseMode(string(m)); err != nil {
		return "", err
	}
	if root != "" && Exists(root) && ValidID(user) {
		l := NewLayout(root)
		p, err := l.LocalPersona(user)
		if err != nil {
			return "", err
		}
		if _, err := EnsureLocalUserDir(l, user); err != nil {
			return "", err
		}
		return p, WriteDoc(p, personaFront{Mode: string(m)}, bodyPersonaMode)
	}
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", err
		}
	}
	p := HomePersonaPath(home)
	front := personaFront{Mode: string(m)}
	if d, err := ReadDoc(p); err == nil && d.HasFront {
		var old personaFront
		if d.Decode(&old) == nil {
			front.Lang = old.Lang // changing the mode keeps the language
		}
	}
	return p, WriteDoc(p, front, bodyPersonaMode)
}

// LoadPersonaLang returns the persona language saved in the home file and whether
// one was saved. A missing, unreadable or invalid file reads as not saved.
func LoadPersonaLang(home string) (persona.Lang, bool) {
	if home == "" {
		return persona.LangES, false
	}
	d, err := ReadDoc(HomePersonaPath(home))
	if err != nil || !d.HasFront {
		return persona.LangES, false
	}
	var f personaFront
	if d.Decode(&f) != nil {
		return persona.LangES, false
	}
	l, err := persona.ParseLang(f.Lang)
	if err != nil {
		return persona.LangES, false
	}
	return l, true
}

// SavePersonaLang stores the persona language in the home file, keeping its chat
// mode, and returns the file written.
func SavePersonaLang(home string, l persona.Lang) (string, error) {
	if _, err := persona.ParseLang(string(l)); err != nil {
		return "", err
	}
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", err
		}
	}
	p := HomePersonaPath(home)
	front := personaFront{Lang: string(l)}
	if d, err := ReadDoc(p); err == nil && d.HasFront {
		var old personaFront
		if d.Decode(&old) == nil {
			front.Mode = old.Mode
		}
	}
	return p, WriteDoc(p, front, bodyPersonaMode)
}

// ActivePersonaLang is the persona language of the person running the command:
// the one saved in the home directory (os.UserHomeDir when home is empty), else es.
func ActivePersonaLang(home string) persona.Lang {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	l, _ := LoadPersonaLang(home)
	return l
}
