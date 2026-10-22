# Web and UI

For pages, apps, components and CSS. Pair with the project's framework conventions.

## Principles

- **Structure first.** Semantic HTML (header, nav, main, section, button, a, label,
  table) before any styling. Headings in order, one h1.
- **Tokens, not literals.** Define custom properties for color roles, type scale,
  spacing, radii, shadows and motion; components consume tokens only.
- **Mobile first, content driven.** Start at a narrow width and add breakpoints
  where the content breaks, not at device names. Fluid type and spacing with
  `clamp()` where it helps; layout with grid and flex.
- **Hierarchy through few levers.** Size and weight first, then color, then
  space. Avoid boxing everything with borders and shadows.
- **Interaction states are design.** Hover, focus-visible, active, disabled,
  loading, selected, error. Focus is never removed without a stronger replacement.
- **Motion is functional.** Transform and opacity only, short durations, honor
  `prefers-reduced-motion`.
- **Theming.** Define light and dark from the same roles; do not invert blindly.
  Dark surfaces use slightly lifted grays, not pure black on pure white text.

## Quality floor

- Body text at least 16 px, line height 1.5, measure 45 to 75 characters.
- Contrast: 4.5:1 body, 3:1 large text and UI boundaries, in every theme.
- Touch targets at least 24 by 24 px with spacing; 44 px for primary mobile actions.
- Works at 320 px wide, at 200 percent zoom, and with large system text.
- No horizontal scroll at any width unless the content is a table or code block.
- Every interactive element reachable and operable by keyboard, in logical order.
- Images have dimensions (no layout shift), alt text, and sensible compression.
- Forms: visible labels, associated errors, correct input types and autocomplete.
- Strict CSP friendly: no inline event handlers; escape any dynamic text.
- Page weight and first paint checked; fonts limited and `font-display` set.

## Layout patterns that work

- A single column of content with a readable max width, wide containers only for
  visuals and tables.
- A 12-column or auto-fit grid with a gap taken from the spacing scale.
- Sections separated by space and a change of background, not by heavy rules.
- Sticky headers only when they are short; never cover the focus target.
- Cards only when items are independent and comparable; a list or table is
  often clearer.

## Type and color

- One family for UI, an optional second for display or reading. System fonts are
  a legitimate choice; web fonts need a reason and a fallback stack.
- A scale of 6 to 8 steps from a ratio between 1.2 and 1.333.
- One brand hue for actions and focus; semantic hues for status; neutrals carry
  the rest. Tint neutrals slightly toward the brand hue for cohesion.
- Prefer perceptually even color definitions (for example OKLCH) when deriving
  scales, then verify contrast on the result.

## Web anti-patterns

- Gradient text or glowing gradient backgrounds as the main visual idea.
- A hero with a huge headline, one gradient button and a floating blob.
- Everything in rounded cards with the same shadow; cards inside cards.
- Glassmorphism (blurred translucent panels) over busy backgrounds, hurting contrast.
- Decorative icons above every heading; a row of three identical feature cards.
- Gray text on colored backgrounds below the contrast minimum.
- Centered everything, including paragraphs.
- Placeholder as label; disabled buttons with no explanation.
- Hover-only information; scroll-jacking; autoplaying media with sound.
- A modal for something that could be inline; a spinner with no end condition.
- Emoji as icons in product UI; icon sets mixed in stroke and style.
- Default framework look shipped unchanged (default font, default blue, default radius).
- Stock-photo smiles, fake testimonials, invented logos, metrics with no source.
- Animations on every element at load.

## Checks before handing over

Render at 360, 768 and 1280 px. Tab through it. Switch theme. Zoom to 200 percent.
Fill with long text and with none. Run the contrast pairs. Report what you tested.
