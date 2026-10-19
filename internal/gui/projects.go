package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ProjectInfo is one project the Manager can open.
type ProjectInfo struct {
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	Sources     []string `json:"sources"`
	HasGabitoTL bool     `json:"hasGabitotl"`
	IsGit       bool     `json:"isGit"`
	LastSeen    string   `json:"lastSeen"` // RFC 3339, "" when unknown
}

// Projects lists the discovered projects (read-only); without the port it is empty.
func (s *Service) Projects(ctx context.Context) ([]ProjectInfo, error) {
	out := []ProjectInfo{}
	if s.d.Projects == nil {
		return out, nil
	}
	list, err := s.d.Projects(ctx)
	if err != nil {
		return out, err
	}
	for _, p := range list {
		pi := ProjectInfo{Path: p.Path, Name: p.Name, Sources: append([]string{}, p.Sources...), HasGabitoTL: p.HasGabitoTL, IsGit: p.IsGit}
		if !p.LastSeen.IsZero() {
			pi.LastSeen = p.LastSeen.UTC().Format(time.RFC3339)
		}
		out = append(out, pi)
	}
	return out, nil
}

// AddProject remembers an existing folder in the Manager's list.
func (s *Service) AddProject(path string) error {
	if s.d.RememberProject == nil {
		return errors.New("gui: no project list configured")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return errors.New("gui: not a folder: " + abs)
	}
	return s.d.RememberProject(abs)
}

// ForgetProject removes a folder from the Manager's list. Nothing on disk is
// touched; a project found by another tool shows up again.
func (s *Service) ForgetProject(path string) error {
	if s.d.ForgetProject == nil {
		return errors.New("gui: no project list configured")
	}
	return s.d.ForgetProject(path)
}
