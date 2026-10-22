package gui

import (
	"context"

	"github.com/winnstorm/gabitotl/internal/installer"
)

// systemInfos converts the installer's read-only findings.
func systemInfos(in []installer.SystemStatus) []SystemInfo {
	out := []SystemInfo{}
	for _, s := range in {
		si := SystemInfo{ID: s.ID, Name: s.Name, URL: s.URL, Conflict: s.Conflict, Action: s.Action, Found: s.Found, Possible: s.Possible, Signals: []SignalInfo{}}
		for _, g := range s.Signals {
			si.Signals = append(si.Signals, SignalInfo{Kind: g.Kind, Target: g.Target, Path: g.Path, Marker: g.Marker, Strong: g.Strong, Found: g.Found})
		}
		out = append(out, si)
	}
	return out
}

// OtherSystems lists the other agent systems of this machine (read-only), found
// or not, for the Manager's Overview and Doctor screens.
func (s *Service) OtherSystems(_ context.Context) ([]SystemInfo, error) {
	env, err := s.env("")
	if err != nil {
		return nil, err
	}
	return systemInfos(installer.DetectSystems(env)), nil
}
