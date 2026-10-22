# Data, charts and spreadsheets

Use together with the xlsx or charting skill that produces the file. The goal is
to show the comparison that matters with the least ink and no ambiguity.

## Principles

- **Start from the question.** What comparison, trend, distribution or
  relationship must the reader see? Choose the form from that answer.
- **Form follows the question.**
  - Compare categories: horizontal or vertical bars from a zero baseline, sorted
    by value unless the order is natural (time, stages).
  - Trend over time: a line; few series; label lines directly at their ends.
  - Part of a whole: a stacked bar, or a pie/donut only with two to four slices.
  - Distribution: histogram, box plot or dot strip; show the sample size.
  - Relationship: scatter plot, with a trend line only if it is justified.
  - Exact values: a table, not a chart.
  - Single key number: a large number with context (change, comparison, period).
- **Data-ink.** Remove what carries no information: heavy gridlines, borders,
  3D, shadows, background fills, redundant legends and axis lines.
- **Honest scales.** Bars start at zero. Do not truncate or dual-axis to
  manufacture drama. Same scale across small multiples. State units and periods.
- **Direct labeling.** Put names at the data, values on key points, a
  title that states the finding, a subtitle for the measure and the period.
- **Emphasis by contrast.** One highlight color for the series or bar that
  carries the message; the rest in neutral gray.
- **Small multiples** beat one chart with many series.
- **Source and date** on every chart that is shared.

## Color

- Categorical: at most five or six distinguishable hues; use a colorblind-safe
  set (for example the Okabe-Ito palette) and add a second cue (direct labels,
  line style, marker shape).
- Sequential: one hue from light to dark for magnitude; diverging with a neutral
  midpoint only when there is a meaningful center (zero, target).
- Semantic colors (red, green) only for status, and never alone: add an icon or text.
- Check contrast of labels on fills; check legibility in grayscale.

## Spreadsheets

- **Separate** inputs, calculations and outputs on distinct, named sheets or
  clearly marked regions. Inputs get a consistent style (for example blue text);
  formulas stay black; never hard-code numbers inside formulas.
- **Header row** frozen, bold, with units; one record per row; no merged cells in
  data ranges; no blank rows or columns inside a table.
- **Formats.** Numbers right-aligned with a consistent number of decimals,
  thousands separators, units in the header, dates in an unambiguous format,
  percentages as percentages, negative numbers clearly shown.
- **Column widths** fitted; long text wrapped in a dedicated column; freeze panes
  for tables larger than a screen.
- **Conditional formatting** sparingly: one rule family, meaningful thresholds, a
  legend note. Banding is light or absent.
- **Named ranges and tables** for formulas that others will read; a short
  "About" sheet explaining purpose, sources, version, and how to update.
- **Checks.** Totals reconcile, no error values, no volatile surprises; protect
  formula cells when sharing.
- **Print setup.** Titles repeat, fit to width, sensible page breaks.

## Dashboards

- Top-left holds the most important number; group related measures; limit to
  about five to nine tiles.
- Each tile answers a question and shows a comparison (vs target, vs last
  period), not a lone number.
- Consistent time range and filters, shown on screen. Filters change everything or
  say what they change.
- Real-time styling only when data is real-time; otherwise show "as of".
- Operate mode applies: density with order, alignment on a grid, no decoration.

## Quality floor

- Title states the finding; axes and units labeled; source and period given.
- Zero baseline for bars; no more than six colors; labels do not overlap.
- Text at least 12 pt in print and 14 px on screen; legible at the size it is shown.
- Accessible: alt text or a data table with the key values, colorblind-safe,
  no meaning by color alone.
- Numbers formatted consistently; rounding disclosed when it matters.

## Data anti-patterns

- 3D bars and pies, exploded slices, shadows, gradients on bars.
- Pie charts with many slices; donuts with the total hidden.
- Dual axes that imply a correlation; truncated bar axes; inconsistent intervals.
- Rainbow palettes for ordered data; red-green only coding.
- A legend far from the data when labels would fit; 12-series line spaghetti.
- Decorative icons and clip art inside charts; background images behind data.
- Default chart styling shipped unchanged, with the default title "Chart 1".
- Dashboards of identical cards with big numbers and no context.
- Spreadsheets with merged header cells, colors with no meaning, and hard-coded totals.
- Invented data presented as real; charts without source; precision beyond the data.

## Check before handing over

Cover the title: is the point still clear? Convert to grayscale. Check the
smallest label at the final size. Recalculate and compare totals. Say which
numbers you could not verify.
