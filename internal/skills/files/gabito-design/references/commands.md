# Commands

Invoke by naming one: "gabito-design critique", "polish the header", "variants of
the hero". Each entry gives the purpose, when to use it, the steps and what done
means. Several commands can chain (for example shape, craft, critique, polish).

## Foundation

### context
- **Purpose:** know the surface before designing, and keep decisions in one place.
- **When:** first design task in a project, or when `DESIGN.md` is missing or stale.
- **Steps:** (1) read `DESIGN.md`, brand files, existing styles, templates;
  (2) answer: audience, job to be done, medium and size, tone in three adjectives,
  constraints (brand, accessibility, tech); (3) ask only what you cannot infer;
  (4) write the result with `references/design-md.md`.
- **Done:** a `DESIGN.md` exists, or the answers are stated in the reply.

### document
- **Purpose:** capture the design decisions already present in finished work.
- **When:** the project has UI or documents but no recorded system.
- **Steps:** read the real styles; list type families and sizes, colors and where
  they are used, spacing values, radii, shadows, motion; merge near-duplicates
  into a proposed scale; note inconsistencies as open items; write `DESIGN.md`.
- **Done:** every token in the file exists in the work, and conflicts are listed.

### extract
- **Purpose:** turn repeated literals into tokens and repeated patterns into components.
- **When:** the same color, size or block appears three or more times.
- **Steps:** find repeats; name them by role (not by value: `text-muted`, not
  `gray-600`); replace in place; keep behavior identical; update `DESIGN.md`.
- **Done:** no behavior change, fewer literals, tokens named by role.

### shape
- **Purpose:** decide structure and content before visuals.
- **When:** new work, or work whose purpose is fuzzy.
- **Steps:** state the one goal; list the content blocks in priority order; write
  the real headline and key copy; sketch the layout in words or boxes (what sits
  where and why); pick the mode; list states to design.
- **Done:** a short brief that someone else could build from.

### craft
- **Purpose:** build a new piece from brief to finished result.
- **When:** new page, screen, document, slide or graphic.
- **Steps:** run context and shape if missing; set tokens; build the structure at
  the main size; add content, then states; apply the module's quality floor;
  render and look at it; run a short critique; fix; report decisions.
- **Done:** it renders, the floor is cleared, and decisions are explained.

## Review

### critique
- **Purpose:** judge the design against its goal, not against taste.
- **When:** the user asks what is wrong, or before a handoff.
- **Steps:** restate the goal and audience; look at it (render it); describe the
  first impression in one line; list findings by impact: goal, hierarchy,
  clarity, consistency, craft; for each, say what, why it matters, and the fix;
  name what already works so it is not lost.
- **Done:** findings ranked, each with a concrete fix; no "make it pop".

### audit
- **Purpose:** check measurable quality.
- **When:** before release, or after large changes.
- **Steps:** contrast of every text and component pair; keyboard path and visible
  focus; semantic structure and reading order; alt text and captions; target
  sizes; responsive behavior at narrow, medium and wide; reduced motion;
  token consistency; weight and load. Report as pass, fail, or not checked.
- **Done:** a list of failures with location and fix, plus what was not checked.

### polish
- **Purpose:** the last 10 percent: details that signal care.
- **When:** structure is right and the piece is nearly done.
- **Steps:** align edges and baselines; fix uneven gaps against the scale; check
  every state (hover, focus, active, disabled, loading, empty, error); fix
  orphans, widows and awkward line breaks; match radii, strokes and icon weight;
  check dark mode and print where relevant.
- **Done:** no visible inconsistency at 100 percent zoom and at the smallest size.

## Reduce and strengthen

### distill
- **Purpose:** remove what does not serve the goal.
- **When:** the design feels busy, long or hesitant.
- **Steps:** for each element ask whether the goal suffers without it; cut copy,
  borders, colors, effects, options; merge near-duplicates; move the rest behind
  progressive disclosure.
- **Done:** fewer elements, the goal is clearer, nothing essential is lost.

### harden
- **Purpose:** make it survive reality.
- **When:** the design only works with ideal content.
- **Steps:** test with very long and very short text, many and zero items,
  missing images, slow or failed loads, other languages and scripts, large text
  settings, small screens, errors and permissions; add truncation, wrapping,
  fallbacks and messages that say what to do next.
- **Done:** each stress case has a deliberate outcome.

### onboard
- **Purpose:** get a new person to the first success.
- **When:** first run, empty states, new features.
- **Steps:** define the first success; remove steps before it; teach by doing
  rather than by tour; design empty states as invitations with one action;
  explain once and let it be dismissed.
- **Done:** the first success is reachable in the fewest steps, and empty states
  say what to do.

### bolder
- **Purpose:** raise energy when the design is timid.
- **When:** everything is the same size, weight and tone.
- **Steps:** pick the one thing to amplify; widen scale contrast; commit to a
  stronger color or crop; simplify around it so it can win.
- **Done:** a clear focal point; the result is louder in one place, not everywhere.

### quieter
- **Purpose:** lower intensity when the design shouts.
- **When:** many competing accents, heavy effects, saturated fills.
- **Steps:** reduce saturation and weight; replace fills with space and hairlines;
  keep one accent; soften motion.
- **Done:** calm, with the primary action still obvious.

## Craft dimensions

### animate
- **Purpose:** motion that explains, not decorates.
- **When:** state changes, transitions, feedback.
- **Steps:** list what the user must understand (where it came from, what
  changed, that it worked); animate only those; 100 to 200 ms for feedback, 200
  to 400 ms for transitions; ease out for entering, ease in for leaving; animate
  transform and opacity; honor reduced motion; keep nothing looping that is not
  a loading state.
- **Done:** each animation answers a question; reduced motion is respected.

### colorize
- **Purpose:** add or repair color with roles.
- **When:** the piece is gray, or color is arbitrary.
- **Steps:** define roles (surface, text, muted text, border, primary, semantic);
  derive tints and shades from one hue family; check contrast per pair; check
  colorblind safety for any color-coded meaning; apply with restraint.
- **Done:** every color has a role and passes contrast.

### typeset
- **Purpose:** choose and tune type.
- **When:** text feels generic, cramped or hard to read.
- **Steps:** decide the voice (neutral, warm, technical, editorial); pick one or
  two families with a reason; set a scale; tune line height, measure, tracking
  (tighter for large, looser for small caps); use real bold and italic; use
  tabular figures for numbers in columns.
- **Done:** a scale with named steps, readable measure, and fallbacks listed.

### layout
- **Purpose:** structure, grid, rhythm and hierarchy.
- **When:** elements feel scattered or equal.
- **Steps:** set the grid and margins; group by proximity; vary section rhythm
  (not every block the same height); use whitespace as structure; check the
  reading path; verify at each breakpoint or page size.
- **Done:** a clear reading path and consistent alignment.

### delight
- **Purpose:** small earned moments of personality.
- **When:** the piece works but feels cold.
- **Steps:** find a moment that already carries emotion (success, empty state,
  first run); add one touch: a line of copy, an illustration, a micro motion;
  keep it skippable and fast.
- **Done:** one or two touches, never in the way of the task.

### overdrive
- **Purpose:** push one piece to an ambitious level.
- **When:** the user explicitly wants a standout showpiece.
- **Steps:** confirm scope and cost (performance, accessibility, maintenance);
  pick one idea (an interaction, a scroll story, a custom visualization); build
  it with a fallback path; keep the rest of the page calm so it can shine.
- **Done:** one ambitious piece that degrades gracefully and respects reduced motion.

## Words and context

### clarify
- **Purpose:** fix confusing copy, labels and instructions.
- **When:** people hesitate, ask, or misuse.
- **Steps:** replace jargon with the user's words; put the action first in
  buttons ("Save changes"); make errors say what happened and what to do; cut
  filler; keep terms consistent.
- **Done:** every label is specific, and each error has a next step.

### adapt
- **Purpose:** carry a design to another size, medium, locale or audience.
- **When:** web to print, desktop to phone, slide to document, English to
  longer languages.
- **Steps:** list what must survive (message, brand, hierarchy); re-derive
  layout for the new constraint instead of scaling; adjust type sizes, density,
  interaction (touch vs pointer), and color (print, grayscale).
- **Done:** it reads as designed for the new context, not shrunk.

### optimize
- **Purpose:** reduce weight and effort.
- **When:** slow load, huge files, heavy documents, dense screens.
- **Steps:** measure first; compress and size images, subset fonts, drop unused
  styles and effects; reduce document and spreadsheet bloat; reduce choices and
  steps for cognitive load.
- **Done:** before and after numbers, no visible quality loss.

### variants
- **Purpose:** compare genuinely different directions.
- **When:** the direction is open, or the user wants options.
- **Steps:** state the axis of difference (layout, tone, density, color
  strategy); make three that differ on that axis, not three tweaks; label each
  with its idea and trade-off; recommend one.
- **Done:** distinct options, labelled, with a recommendation.
