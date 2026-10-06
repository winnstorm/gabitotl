package installer

import (
	"context"
	"encoding/json"
)

// Event is one progress notification. The installer never prints; presenters
// subscribe through an Observer.
type Event struct {
	Phase  Phase
	Status string // start | ok | warn | error | info
	Step   string
	Detail string
}

// Observer receives progress events. Nil is allowed everywhere.
type Observer func(Event)

func (o Observer) emit(p Phase, status, step, detail string) {
	if o != nil {
		o(Event{Phase: p, Status: status, Step: step, Detail: detail})
	}
}

// InventoryVersion is the schema version of inventory.json.
const InventoryVersion = 1

// Inventory is everything the detection phases found. It is what the global
// snapshot stores as inventory.json, so it never carries secrets, remotes or
// environment values (invariant f).
type Inventory struct {
	Version   int             `json:"version"`
	CreatedAt string          `json:"created_at"`
	Platform  Platform        `json:"platform"`
	Project   Project         `json:"project"`
	Runtimes  []RuntimeStatus `json:"runtimes"`
	Memory    MemoryInfo      `json:"memory"`
	Other     OtherHarness    `json:"other_harness"`
}

// BuildInventory runs the detection phases in order (spec 5, steps 1 to 6).
// Everything it does is read-only.
func BuildInventory(ctx context.Context, env Env, obs Observer) Inventory {
	inv := Inventory{Version: InventoryVersion, CreatedAt: env.Clock().Format("2006-01-02T15:04:05Z07:00")}
	obs.emit(PhasePlatform, "start", "detect platform", "")
	inv.Platform = DetectPlatform(env)
	obs.emit(PhaseProject, "start", "detect project", "")
	inv.Project = DetectProject(ctx, env)
	obs.emit(PhaseRuntimes, "start", "detect runtimes", "")
	inv.Runtimes = DetectRuntimes(env)
	obs.emit(PhaseMemory, "start", "detect memory", "")
	inv.Memory = DetectMemory(ctx, env, inv.Project)
	for _, w := range inv.Memory.Warnings {
		obs.emit(PhaseMemory, "warn", w.Code, w.Detail)
	}
	obs.emit(PhaseOtherHarness, "start", "detect other harness", "")
	inv.Other = DetectOtherHarness(env)
	obs.emit(PhaseInventory, "ok", "inventory built", "")
	return inv
}

// JSON renders the inventory for the snapshot.
func (i Inventory) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Runtime returns the detected runtime with the given name.
func (i Inventory) Runtime(name string) (RuntimeStatus, bool) {
	for _, r := range i.Runtimes {
		if r.Name == name {
			return r, true
		}
	}
	return RuntimeStatus{}, false
}
