package installer

import (
	"context"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/importer"
	"github.com/winnstorm/gabitotl/internal/memory"
)

var entryTypes = map[importer.Kind]memory.EntryType{
	importer.KindDecision:     memory.TypeDecision,
	importer.KindConvention:   memory.TypeConvention,
	importer.KindArchitecture: memory.TypeDiscovery,
	importer.KindTask:         memory.TypePending,
	importer.KindCommand:      memory.TypeConfig,
	importer.KindConstraint:   memory.TypeConvention,
	importer.KindTooling:      memory.TypeConfig,
}

// KnowledgeSink saves imported knowledge through a memory provider. Saving goes
// through the provider (for Engram, the official CLI): the Engram database file
// is never touched directly.
//
// Imported authorship is unknown, so it is never invented. Which side owns the
// actor depends on the provider:
//
//   - memory-mcp talks to an authenticated server that derives the actor from the
//     token and rejects any other (a claimed "legacy" can never authenticate). So
//     no actor is sent: the entry is attributed to whoever runs the import, and the
//     provenance lives in Source.
//   - every other provider records the actor "legacy" in team mode.
func KnowledgeSink(p memory.Provider, cfg gabitotl.MemoryConfig) func(context.Context, importer.Knowledge) error {
	mode, actor := cfg.Mode, memory.ActorLegacy
	if cfg.Provider == gabitotl.MemoryMemoryMCP {
		// The actor rule of team mode is enforced by the server from the token; the
		// local guard must not demand one here.
		mode, actor = gabitotl.ModeSolo, ""
	}
	g := memory.Guard{Provider: p, Mode: mode}
	return func(ctx context.Context, k importer.Knowledge) error {
		_, err := g.Save(ctx, memory.Entry{
			Type: entryTypes[k.Kind], Title: k.Title, Project: cfg.ProjectKey,
			Content:  k.Body + "\n\nImported from " + k.Origin + " (" + k.Path + "). Historical context: current code and configuration win.",
			TopicKey: "import/" + k.Origin + "/" + gabitotl.Slug(k.Title),
			Actor:    actor, Source: "gabitotl-import",
		})
		return err
	}
}

// probeTimeout bounds the read-only reachability probe of the team memory.
const probeTimeout = 10 * time.Second

// teamNotReady asks a memory-mcp provider whether it can take writes right now.
// It returns "" when it can, otherwise a short reason; a server that does not
// answer is reported as "unreachable". It never writes.
func teamNotReady(ctx context.Context, p memory.Provider) string {
	dctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	for _, c := range p.Doctor(dctx).Checks {
		if c.Status != memory.StatusError {
			continue
		}
		if c.Name == "server" {
			return "unreachable"
		}
		return c.Name + ": " + c.Message
	}
	return ""
}
