# DESIGN.md: the project's recorded design decisions

`DESIGN.md` sits at the project root. It lets every later task reuse the same
decisions instead of re-inventing them. Keep it short, specific and current. Read
it before designing; update it when a decision changes; never let it contradict
the code (generate from the code when documenting an existing project).

## When to write it

- The `context` command runs for the first time in a project.
- The `document` command captures an existing design.
- A new reusable decision appears (a new color role, a new component).

## Template

Copy this structure, fill every field with a real value or delete the line. Use
names that describe roles, not values.

````markdown
# Design

## Context
- **Product / surface:** what it is, for whom
- **Mode:** Persuade | Experience | Operate | Read (and the secondary one, if any)
- **Tone:** three adjectives, and one thing it must never feel like
- **Constraints:** brand files, accessibility target (for example WCAG 2.2 AA),
  supported browsers or office apps, performance budget, languages

## Principles
1. One sentence each, 3 to 5 in total, each testable ("Primary action is always
   visible without scrolling")

## Color
| Role | Light | Dark | Use |
|---|---|---|---|
| surface | #FFFFFF | #14161A | page background |
| surface-raised | | | cards, menus |
| text | | | body text |
| text-muted | | | secondary text (contrast checked: ratio) |
| border | | | hairlines |
| primary | | | actions, links, focus |
| on-primary | | | text on primary |
| success / warning / danger | | | status, always with icon or text |

## Typography
- **Families:** display / body / mono, with fallback stacks and license notes
- **Scale:** name, size, line height, weight, use (for example `body 16/24 400`,
  `h1 40/48 700`); the ratio used
- **Rules:** measure, numerals (tabular in tables), capitalization, hyphenation

## Spacing and layout
- **Base unit and scale:** for example 4: 4, 8, 12, 16, 24, 32, 48, 64
- **Grid:** columns, gutters, margins, max content width, breakpoints or page sizes
- **Density:** comfortable | compact, and where each applies

## Shape and elevation
- **Radii:** for example sm 4, md 8, lg 16, full
- **Borders:** width and color roles
- **Shadows:** at most two levels and their use (or "none")

## Motion
- **Durations:** feedback 120 ms, transition 240 ms
- **Easing:** names and curves
- **Rules:** what may animate, reduced-motion behavior

## Components and patterns
- A short list of the reusable pieces (button, input, card, table, chart style,
  slide layouts, document styles) with where they live in the code or template

## Do and don't
- Do: concrete examples from this project
- Don't: the anti-patterns that tempted this project, with the reason

## Open questions
- Decisions not yet made, inconsistencies found, who decides
````

## Adapting to the medium

- **Web:** tokens map to CSS custom properties; note the file that defines them.
- **Documents:** record the style names in the template (Heading 1, Body, Caption)
  and the page setup; keep the template path.
- **Presentations:** record master layouts and the title position; keep the
  template path.
- **Data and charts:** record the categorical palette, the highlight color, number
  and date formats, and the source line format.
- **Images and logos:** record the mark files, clear space, minimum size, the
  illustration style and the license of each asset.

## Maintenance rules

- If a value in `DESIGN.md` and the code disagree, the code is the fact: fix the file
  or raise the inconsistency under Open questions.
- Review it at the end of each design task: did you add a decision others should reuse?
- Keep it under about 200 lines; link out to larger inventories.
