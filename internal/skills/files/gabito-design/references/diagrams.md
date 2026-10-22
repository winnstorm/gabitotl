# Diagrams (flows, architecture, maps, sequences)

A diagram is worth drawing when relationships or order are the point and a
paragraph would force the reader to build the picture alone.

## Principles

- **One diagram, one question.** "What talks to what?", "In what order?", "Who
  decides?", "How does data move?" Put the question in the title. If two
  questions appear, draw two diagrams.
- **Choose the type from the question.**
  - Order of steps and decisions: flowchart.
  - Interactions over time between actors: sequence diagram.
  - Components and their connections: block or architecture diagram.
  - Hierarchy: tree or org chart.
  - States and transitions: state diagram.
  - Data structures and relations: entity-relationship diagram.
  - Timeline or plan: Gantt or a simple timeline.
- **Reading direction.** Flow left to right or top to bottom, consistently. The
  main path is straight; exceptions branch away.
- **Few nodes.** Aim for 5 to 9 per diagram; group the rest into labeled
  containers or split into levels (overview, then detail).
- **Meaningful shapes.** Use a small, consistent vocabulary: one shape per kind of
  thing, a legend only when the vocabulary is not obvious. Decisions are
  diamonds only in true flowcharts.
- **Label lines, not just boxes.** Say what travels along an arrow ("order
  event", "HTTPS"). Arrow direction is meaningful; avoid double arrows unless
  truly bidirectional.
- **Alignment and spacing.** Snap to a grid, equal gaps, no crossing lines where
  avoidable; when lines must cross, use a clear hop or junction.
- **Color for grouping or status,** two to four colors, with text labels as well.
- **Text.** Box labels at 12 to 16 pt equivalent, short noun phrases or verbs,
  sentence case, no paragraphs inside boxes.

## Quality floor

- The title states the question or conclusion; the reader can tell the start
  and the end within three seconds.
- No orphan nodes, dangling arrows or unlabeled ambiguous connections.
- Consistent stroke width, corner radius, arrowheads and font across the diagram.
- Contrast of text on fills at least 4.5:1; meaning survives grayscale.
- Legible at the size where it will be used (slide, page, wiki); exports at
  vector or at least twice the display resolution.
- Text-based sources (Mermaid, PlantUML, Graphviz, SVG) kept in the repo next
  to the exported image so it can be edited.
- Alt text or an adjacent text summary describes the structure.

## Choosing the tooling

- For documentation in a repository, prefer text-based diagrams that diff well
  and render in the host (Mermaid is widely supported).
- For slides, draw native shapes or export a clean SVG; avoid screenshots of tools.
- For web, inline SVG with `currentColor` and CSS variables adapts to themes.
- Verify the render. A diagram that was never rendered is a guess.

## Diagram anti-patterns

- Everything in the system on one canvas: the "wall of boxes".
- Boxes with logos of every vendor in a different color and size.
- Gradient fills, shadows, 3D blocks and clip-art icons for decoration.
- Rainbow arrows with no meaning; arrows with no labels; crossing spaghetti.
- Diagrams that restate the table of contents instead of showing relationships.
- Inconsistent shapes: the same kind of thing as a box here, a cloud there.
- Tiny text to fit more in; boxes with full sentences.
- Hand-drawn style mixed with strict geometry without intention.
- A legend as large as the diagram; color-only distinctions.
- Auto-layout output left untouched with overlapping labels.
- Fictional components or flows that the real system does not have.

## Check before handing over

Render it. Trace the main path with a finger. Remove one box: does the diagram
still answer the question? If yes, remove it. Report what was drawn from facts
you verified and what you assumed.
