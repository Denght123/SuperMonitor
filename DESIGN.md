---
name: SuperMonitor
description: A light-first mission-control console for trustworthy AI quota signals, with a graphite dark mode.
colors:
  canvas: "#f5f7fa"
  instrument-surface: "#ffffff"
  raised-surface: "#f0f3f7"
  etched-line: "#dce2ea"
  primary-text: "#18212f"
  secondary-text: "#4b596c"
  muted-text: "#6d7a8c"
  live-teal: "#137a72"
  signal-blue: "#3478d4"
  warning-amber: "#c77a14"
  healthy-green: "#15956f"
  fault-coral: "#d6474d"
typography:
  display:
    fontFamily: "Manrope Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "28–34px"
    fontWeight: 700
    lineHeight: 1.15
    letterSpacing: "-0.028em"
  body:
    fontFamily: "Manrope Variable, ui-sans-serif, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.5
  data:
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace"
    fontSize: "18px"
    fontWeight: 700
    lineHeight: 1.1
rounded:
  control: "9px"
  navigation: "10px"
  surface: "14px"
spacing:
  xs: "5px"
  sm: "8px"
  md: "14px"
  lg: "20px"
  page: "30px"
components:
  button-primary:
    backgroundColor: "{colors.live-cyan}"
    textColor: "{colors.graphite-bg}"
    rounded: "{rounded.control}"
    padding: "0 14px"
    height: "38px"
  button-secondary:
    backgroundColor: "{colors.instrument-surface}"
    textColor: "{colors.secondary-text}"
    rounded: "{rounded.control}"
    padding: "0 12px"
  panel:
    backgroundColor: "{colors.instrument-surface}"
    textColor: "{colors.primary-text}"
    rounded: "{rounded.surface}"
    padding: "18px"
---

# Design System: SuperMonitor

## Overview

**Creative North Star: "The Readable Signal Bench"**

SuperMonitor is an operator's instrument panel rather than a generic SaaS card wall. It defaults to a quiet light workspace for long monitoring sessions, with graphite dark mode available on demand. Fine separators, generous type, and deliberately scarce teal live signals make freshness, quota pressure, and source confidence readable at a glance.

The interface is dense but calm. Charts establish the first visual priority, native provider units remain intact, and motion communicates continuity or state changes without delaying work. Decorative imagery is intentionally absent; the data and its provenance are the visual material.

**Key Characteristics:**

- Chart-first operational hierarchy.
- Light tonal layering with fine structural borders and an equivalent graphite dark mode.
- Teal for live actions, amber for caution, coral for faults, and green for healthy connections.
- Monospaced numerals only for measurements, clocks, and keyboard hints.
- Fast, state-driven motion with a complete reduced-motion path.

## Colors

The palette behaves like a dim technical workspace: neutral graphite carries the interface while signal colors remain rare and semantic.

### Primary

- **Live Cyan:** Marks the current connection, primary action, selected data range, and trustworthy live state.

### Secondary

- **Signal Blue:** Separates comparison series and secondary quota metrics without competing with the primary cyan.

### Tertiary

- **Warning Amber:** Indicates approaching limits and the request-count trace.
- **Healthy Green:** Indicates connected, encrypted, and successful states.
- **Fault Coral:** Identifies critical quota and refresh failures; it is always paired with text or an icon.

### Neutral

- **Graphite Background:** The deepest continuous canvas.
- **Instrument Surface:** The standard data panel and control surface.
- **Raised Surface:** Active navigation, hover response, tooltips, and command overlays.
- **Etched Line:** Fine structure between readings; never a decorative heavy outline.
- **Primary, Secondary, and Muted Text:** Three deliberate information tiers.

**The Signal Scarcity Rule.** Cyan is reserved for live state, selection, and the primary action; it must not become general decoration.

## Typography

- **Display Font:** Manrope Variable with system sans fallbacks
- **Body Font:** Manrope Variable with system sans fallbacks
- **Label/Mono Font:** SFMono-Regular or Consolas with a monospace fallback

**Character:** Manrope keeps dense Chinese and English interface copy clear and contemporary. The mono stack is functional, not thematic: it belongs to measured values, clocks, shortcut keys, and tabular figures.

### Hierarchy

- **Display** (700, 28–34px, 1.15): Page titles and the strongest current-context label.
- **Headline** (700, 18px, compact): Panel and section headings.
- **Title** (700, 15–16px): Account names, setting titles, and alert titles.
- **Body** (400, 14px, 1.5): Operational descriptions and supporting state.
- **Label** (500–700, 11–13px): Sources, legends, statuses, and compact metadata.
- **Data** (700, 18–23px, tabular numerals): Quotas, balances, KPIs, and the data clock.

**The Measurement Voice Rule.** Use monospace only where alignment or numeric scanning materially improves comprehension.

## Layout

Desktop uses a 224px persistent navigation rail and a fluid main stage. The main page rhythm is 30px at wide sizes, 14px between major instruments, and tighter spacing inside a related signal group. The first viewport gives the token trace the broad column and the model distribution the narrow column.

Below 1120px the navigation collapses to an icon rail. Below 840px it becomes a fixed six-item bottom dock, panels stack to one column, account rows reform into compact two-column summaries, and the page keeps enough bottom padding to avoid dock overlap. Horizontal scrolling is reserved for the native quota rail, where preserving one signal per unit is more important than squeezing content.

## Elevation & Depth

The system is flat by default and uses tonal layering plus one-pixel etched borders for structure. Shadows appear only where a surface truly floats: the brand mark, account drawer, command palette, tooltip, and transient event message. Blur is functional on sticky or modal layers, not ambient decoration.

**The Structural Depth Rule.** Resting content panels use borders and tonal contrast; shadows are reserved for overlays and transient elevation.

## Shapes

Main instrument surfaces use gently rounded 14px corners. Navigation items use 10px corners, controls use 9px, and tiny tags may use compact capsules when their role is metadata rather than a container. Progress tracks remain narrow and squared enough to read as measurements, not decorative pills.

## Components

### Buttons

- **Shape:** Compact controls with 9px corners and a 38px primary height.
- **Primary:** Live cyan fill with graphite text; reserved for the page's immediate action.
- **Hover / Focus:** Tonal lift or border-color shift, a visible 2px cyan focus ring, and a subtle one-pixel active press.
- **Disabled:** Clearly reduced opacity with a not-allowed cursor and no active motion.

### Chips

- **Style:** Fine border, deep surface fill, compact label typography.
- **State:** Selected ranges use a restrained cyan tint and cyan border; source chips remain neutral unless official provenance is being called out.

### Cards / Containers

- **Corner Style:** 14px for major instruments; smaller radii only for nested controls.
- **Background:** Instrument surface over the graphite canvas.
- **Shadow Strategy:** None at rest; overlays alone receive ambient shadows.
- **Border:** One-pixel etched line or softer internal separator.
- **Internal Padding:** Typically 14–18px, increasing only around page-level groups.

### Inputs / Fields

- **Style:** Borderless input inside a structured command-panel row.
- **Focus:** The enclosing interaction remains visible and keyboard focus retains the cyan ring.
- **Error / Disabled:** Fault coral is paired with recovery copy; disabled controls remain visibly unavailable.

### Navigation

The active destination uses a raised graphite surface, etched border, and cyan icon. Desktop labels remain left-aligned and scan quickly; mobile converts the same six destinations into a fixed bottom dock with 47px touch targets.

### Quota Signal Rail

Each provider retains its own unit, source, freshness, and reset or expiry timing. Progress bars are used only where a meaningful total exists; balances and credits are never forced into a false universal percentage.

## Do's and Don'ts

### Do:

- **Do** preserve provider-native units and show source and freshness beside the value.
- **Do** use semantic status color together with text, icons, or placement.
- **Do** keep routine transitions fast, interruptible, and transform-based.
- **Do** test the 224px rail, 76px rail, and mobile bottom-dock compositions.

### Don't:

- **Don't** replace the chart-first hierarchy with a generic grid of equal cards.
- **Don't** mix credits, currency, token plans, and time-window limits into one score.
- **Don't** use cyan as ambient decoration or coral without a recovery path.
- **Don't** add proxy-routing, account-rotation, or model-request controls to this product surface.
