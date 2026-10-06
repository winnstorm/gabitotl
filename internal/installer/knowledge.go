package installer

import (
	"context"

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

// KnowledgeSink saves imported knowledge through a memory provider. Imported
// authorship is unknown, so team mode records the actor as "legacy" (never
// invented). Saving goes through the provider (for Engram, the official CLI):
// the Engram database file is never touched directly.
func KnowledgeSink(p memory.Provider, cfg gabitotl.MemoryConfig) func(context.Context, importer.Knowledge) error {
	g := memory.Guard{Provider: p, Mode: cfg.Mode}
	return func(ctx context.Context, k importer.Knowledge) error {
		_, err := g.Save(ctx, memory.Entry{
			Type: entryTypes[k.Kind], Title: k.Title, Project: cfg.ProjectKey,
			Content:  k.Body + "\n\nImported from " + k.Origin + " (" + k.Path + "). Historical context: current code and configuration win.",
			TopicKey: "import/" + k.Origin + "/" + gabitotl.Slug(k.Title),
			Actor:    memory.ActorLegacy, Source: "gabitotl-import",
		})
		return err
	}
}
