package pgmanaged

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// EngineInfo identifies the container engine in use.
type EngineInfo struct {
	Name    string // docker or podman
	Path    string
	Version string
}

// Detect finds a usable engine: the requested one, or docker then podman for
// "auto". "Usable" means the binary exists and its daemon answers.
func (m *Manager) Detect(ctx context.Context) (EngineInfo, error) {
	candidates := []string{m.Cfg.Engine}
	if m.Cfg.Engine == "auto" {
		candidates = []string{"docker", "podman"}
	}
	var problems []string
	for _, name := range candidates {
		path, err := m.LookPath(name)
		if err != nil {
			problems = append(problems, name+": not found in PATH")
			continue
		}
		format := "{{.Server.Version}}"
		if name == "podman" {
			format = "{{.Version}}"
		}
		res, err := m.Run.Run(ctx, Call{Bin: path, Args: []string{"version", "--format", format}})
		if err != nil {
			problems = append(problems, name+": daemon not reachable ("+firstLine(err.Error())+")")
			continue
		}
		return EngineInfo{Name: name, Path: path, Version: strings.TrimSpace(string(res.Stdout))}, nil
	}
	return EngineInfo{}, fmt.Errorf("%w: %s", ErrNoEngine, strings.Join(problems, "; "))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// resource is the common part of an inspected container, volume or network.
type resource struct {
	Exists  bool
	Managed bool // carries io.gabitotl.managed=true
	ID      string
	Labels  map[string]string
}

type containerInfo struct {
	resource
	Name     string
	Running  bool
	Status   string
	Health   string
	Image    string
	HostIP   string
	HostPort int
	Volumes  []string // named volumes mounted
	Networks []string
}

// call runs an engine command.
func (m *Manager) call(ctx context.Context, e EngineInfo, env []string, args ...string) (Result, error) {
	return m.Run.Run(ctx, Call{Bin: e.Path, Args: args, Env: env})
}

// absent reports whether an inspect failure means "no such object".
func absent(err error) bool {
	var ee *ExitError
	if !errors.As(err, &ee) {
		return false
	}
	l := strings.ToLower(ee.Stderr)
	return strings.Contains(l, "no such") || strings.Contains(l, " not found")
}

func ownedLabels(l map[string]string) bool { return l[LabelManaged] == "true" }

func (m *Manager) inspectContainer(ctx context.Context, e EngineInfo) (containerInfo, error) {
	res, err := m.call(ctx, e, nil, "inspect", "--type", "container", m.Cfg.Container)
	if err != nil {
		if absent(err) {
			return containerInfo{}, nil
		}
		return containerInfo{}, fmt.Errorf("pgmanaged: inspect container: %w", err)
	}
	var raw []struct {
		ID     string `json:"Id"`
		Name   string
		Config struct {
			Labels map[string]string
			Image  string
		}
		State struct {
			Status  string
			Running bool
			Health  *struct{ Status string }
		}
		HostConfig struct {
			PortBindings map[string][]struct{ HostIp, HostPort string }
		}
		Mounts          []struct{ Type, Name string }
		NetworkSettings struct {
			Networks map[string]json.RawMessage
		}
	}
	if err := json.Unmarshal(res.Stdout, &raw); err != nil || len(raw) != 1 {
		return containerInfo{}, errors.New("pgmanaged: unexpected inspect output for the container")
	}
	c := raw[0]
	// The engine resolves names and ID prefixes; insist on the exact name.
	if strings.TrimPrefix(c.Name, "/") != m.Cfg.Container {
		return containerInfo{}, nil
	}
	ci := containerInfo{
		resource: resource{Exists: true, Managed: ownedLabels(c.Config.Labels), ID: c.ID, Labels: c.Config.Labels},
		Name:     m.Cfg.Container, Running: c.State.Running, Status: c.State.Status, Image: c.Config.Image,
	}
	if c.State.Health != nil {
		ci.Health = c.State.Health.Status
	}
	if b := c.HostConfig.PortBindings[strconv.Itoa(containerPort)+"/tcp"]; len(b) > 0 {
		ci.HostIP = b[0].HostIp
		ci.HostPort, _ = strconv.Atoi(b[0].HostPort)
	}
	for _, mt := range c.Mounts {
		if mt.Type == "volume" {
			ci.Volumes = append(ci.Volumes, mt.Name)
		}
	}
	for n := range c.NetworkSettings.Networks {
		ci.Networks = append(ci.Networks, n)
	}
	return ci, nil
}

func (m *Manager) inspectNamed(ctx context.Context, e EngineInfo, kind, name string) (resource, error) {
	res, err := m.call(ctx, e, nil, kind, "inspect", name)
	if err != nil {
		if absent(err) {
			return resource{}, nil
		}
		return resource{}, fmt.Errorf("pgmanaged: inspect %s: %w", kind, err)
	}
	var raw []struct {
		Name   string
		ID     string `json:"Id"`
		Labels map[string]string
	}
	if err := json.Unmarshal(res.Stdout, &raw); err != nil || len(raw) != 1 {
		return resource{}, fmt.Errorf("pgmanaged: unexpected inspect output for the %s", kind)
	}
	if raw[0].Name != name {
		return resource{}, nil
	}
	return resource{Exists: true, Managed: ownedLabels(raw[0].Labels), ID: raw[0].ID, Labels: raw[0].Labels}, nil
}
