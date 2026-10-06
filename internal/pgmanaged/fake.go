package pgmanaged

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
)

// FakeEngine is an in-memory container engine for tests. It speaks the subset
// of the docker CLI that Manager uses and records every mutation together with
// whether the target carried our label at that moment, which is what the
// isolation tests assert on.
type FakeEngine struct {
	mu sync.Mutex
	// Down simulates an unreachable daemon.
	Down bool
	// NotReady makes pg_isready fail.
	NotReady bool
	// PoolExhausted makes `network create` fail unless an explicit --subnet is given.
	PoolExhausted bool
	// FailRun makes `run` fail after creating the container (as a bind error would).
	FailRun    bool
	Containers map[string]*FakeContainer
	Volumes    map[string]map[string]string
	Networks   map[string]map[string]string
	// Calls holds every call in order (arguments and environment names only).
	Calls     []Call
	Mutations []Mutation
	nextID    int
}

// FakeContainer is a container of the fake engine.
type FakeContainer struct {
	ID, Name, Image string
	Labels          map[string]string
	Running         bool
	HostIP          string
	HostPort        string
	Volumes         []string
	Env             []string
}

// Mutation is a state-changing command and the label status of its target.
type Mutation struct {
	Verb    string // create-container, start, stop, rm, volume-create, volume-rm, network-create, network-rm
	Name    string
	Labeled bool
}

var _ Runner = (*FakeEngine)(nil)

// NewFakeEngine returns an engine with nothing in it.
func NewFakeEngine() *FakeEngine {
	return &FakeEngine{Containers: map[string]*FakeContainer{}, Volumes: map[string]map[string]string{}, Networks: map[string]map[string]string{}}
}

func fail(code int, msg string) error { return &ExitError{Code: code, Stderr: msg} }

func (f *FakeEngine) byRef(ref string) *FakeContainer {
	for _, c := range f.Containers {
		if c.Name == ref || c.ID == ref {
			return c
		}
	}
	return nil
}

func (f *FakeEngine) mutate(verb, name string, labels map[string]string) {
	f.Mutations = append(f.Mutations, Mutation{Verb: verb, Name: name, Labeled: ownedLabels(labels)})
}

func parseLabels(args []string) map[string]string {
	l := map[string]string{}
	for i, a := range args {
		if a == "--label" && i+1 < len(args) {
			k, v, _ := strings.Cut(args[i+1], "=")
			l[k] = v
		}
	}
	return l
}

// Run implements Runner.
func (f *FakeEngine) Run(_ context.Context, c Call) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Bin: c.Bin, Args: slices.Clone(c.Args), Env: envNames(c.Env)})
	if f.Down {
		return Result{ExitCode: 1}, fail(1, "Cannot connect to the Docker daemon at unix:///var/run/docker.sock")
	}
	a := c.Args
	if len(a) == 0 {
		return Result{}, fail(1, "no command")
	}
	switch a[0] {
	case "version":
		return Result{Stdout: []byte("28.5.1\n")}, nil
	case "inspect":
		ref := a[len(a)-1]
		ct := f.byRef(ref)
		if ct == nil {
			return Result{Stdout: []byte("[]\n")}, fail(1, "Error response from daemon: No such container: "+ref)
		}
		return f.inspectJSON(ct)
	case "volume", "network":
		return f.named(a, c)
	case "run":
		return f.run(a, c)
	case "start", "stop", "rm", "logs", "exec":
		return f.onContainer(a, c)
	}
	return Result{}, fail(1, "unsupported fake command "+a[0])
}

func envNames(env []string) []string {
	out := make([]string, len(env))
	for i, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		out[i] = k + "=<redacted>"
	}
	return out
}

func (f *FakeEngine) inspectJSON(ct *FakeContainer) (Result, error) {
	mounts := []map[string]string{}
	for _, v := range ct.Volumes {
		mounts = append(mounts, map[string]string{"Type": "volume", "Name": v})
	}
	pb := map[string]any{}
	if ct.HostPort != "" {
		pb["5432/tcp"] = []map[string]string{{"HostIp": ct.HostIP, "HostPort": ct.HostPort}}
	}
	out := []any{map[string]any{
		"Id": ct.ID, "Name": "/" + ct.Name,
		"Config":          map[string]any{"Labels": ct.Labels, "Image": ct.Image},
		"State":           map[string]any{"Status": map[bool]string{true: "running", false: "exited"}[ct.Running], "Running": ct.Running},
		"HostConfig":      map[string]any{"PortBindings": pb},
		"Mounts":          mounts,
		"NetworkSettings": map[string]any{"Networks": map[string]any{}},
	}}
	b, _ := json.Marshal(out)
	return Result{Stdout: b}, nil
}

func (f *FakeEngine) named(a []string, c Call) (Result, error) {
	set := f.Volumes
	if a[0] == "network" {
		set = f.Networks
	}
	if len(a) < 3 {
		return Result{}, fail(1, "usage")
	}
	name := a[len(a)-1]
	switch a[1] {
	case "inspect":
		l, ok := set[name]
		if !ok {
			msg := "Error response from daemon: get " + name + ": no such volume"
			if a[0] == "network" {
				msg = "Error response from daemon: network " + name + " not found"
			}
			return Result{Stdout: []byte("[]\n")}, fail(1, msg)
		}
		b, _ := json.Marshal([]any{map[string]any{"Name": name, "Id": "id-" + name, "Labels": l}})
		return Result{Stdout: b}, nil
	case "create":
		if _, ok := set[name]; ok {
			return Result{}, fail(1, "already exists")
		}
		if a[0] == "network" && f.PoolExhausted && !slices.Contains(a, "--subnet") {
			return Result{}, fail(1, "Error response from daemon: all predefined address pools have been fully subnetted")
		}
		l := parseLabels(a)
		set[name] = l
		f.mutate(a[0]+"-create", name, l)
		return Result{Stdout: []byte(name + "\n")}, nil
	case "rm":
		l, ok := set[name]
		if !ok {
			return Result{}, fail(1, "Error: No such "+a[0]+": "+name)
		}
		f.mutate(a[0]+"-rm", name, l)
		delete(set, name)
		return Result{}, nil
	}
	return Result{}, fail(1, "unsupported")
}

func (f *FakeEngine) run(a []string, c Call) (Result, error) {
	ct := &FakeContainer{Labels: parseLabels(a), Image: a[len(a)-1], Running: true}
	for i := 1; i < len(a)-1; i++ {
		switch a[i] {
		case "--name":
			ct.Name = a[i+1]
		case "-p":
			parts := strings.Split(a[i+1], ":")
			ct.HostIP, ct.HostPort = parts[0], parts[1]
		case "-v":
			v, _, _ := strings.Cut(a[i+1], ":")
			ct.Volumes = append(ct.Volumes, v)
		}
	}
	if f.byRef(ct.Name) != nil {
		return Result{}, fail(125, "Conflict. The container name \"/"+ct.Name+"\" is already in use")
	}
	for _, o := range f.Containers {
		if ct.HostPort != "" && o.HostPort == ct.HostPort && o.Running {
			return Result{}, fail(125, "port is already allocated")
		}
	}
	f.nextID++
	ct.ID = fmt.Sprintf("%064x", f.nextID)
	ct.Env = envNames(c.Env)
	f.Containers[ct.Name] = ct
	f.mutate("create-container", ct.Name, ct.Labels)
	if f.FailRun {
		ct.Running = false
		return Result{}, fail(125, "driver failed programming external connectivity")
	}
	return Result{Stdout: []byte(ct.ID + "\n")}, nil
}

func (f *FakeEngine) onContainer(a []string, c Call) (Result, error) {
	var ref string
	switch a[0] {
	case "rm":
		ref = a[len(a)-1]
	case "logs":
		ref = a[len(a)-1]
	default:
		ref = a[1]
	}
	ct := f.byRef(ref)
	if ct == nil {
		return Result{}, fail(1, "Error response from daemon: No such container: "+ref)
	}
	switch a[0] {
	case "start":
		f.mutate("start", ct.Name, ct.Labels)
		ct.Running = true
	case "stop":
		f.mutate("stop", ct.Name, ct.Labels)
		ct.Running = false
	case "rm":
		f.mutate("rm", ct.Name, ct.Labels)
		delete(f.Containers, ct.Name)
	case "logs":
		f.mutate("logs", ct.Name, ct.Labels)
		return Result{Stderr: []byte("LOG: database system is ready to accept connections\n")}, nil
	case "exec":
		f.mutate("exec", ct.Name, ct.Labels)
		if !ct.Running {
			return Result{}, fail(1, "container is not running")
		}
		if len(a) > 2 && a[2] == "pg_isready" && f.NotReady {
			return Result{}, fail(2, "no response")
		}
		if len(a) > 2 && a[2] == "pg_dump" && c.Stdout != nil {
			_, _ = io.WriteString(c.Stdout, "PGDMP-fake-dump")
		}
	}
	return Result{}, nil
}

// Snapshot returns a comparable description of every resource that does not
// carry our label, so tests can prove such resources never change.
func (f *FakeEngine) Snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for n, c := range f.Containers {
		if !ownedLabels(c.Labels) {
			out = append(out, fmt.Sprintf("container %s id=%s running=%v port=%s", n, c.ID, c.Running, c.HostPort))
		}
	}
	for n, l := range f.Volumes {
		if !ownedLabels(l) {
			out = append(out, "volume "+n)
		}
	}
	for n, l := range f.Networks {
		if !ownedLabels(l) {
			out = append(out, "network "+n)
		}
	}
	slices.Sort(out)
	return out
}

// Unlabeled lists the verbs that targeted a resource without our label.
func (f *FakeEngine) Unlabeled() []Mutation {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Mutation
	for _, m := range f.Mutations {
		if !m.Labeled {
			out = append(out, m)
		}
	}
	return out
}
