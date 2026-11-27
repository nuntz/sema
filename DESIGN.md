---
name: Sema
description: Triage your feeds at a glance.
colors:
  marker-lime: "#d6f24b"
  marker-lime-hover: "#c8e63c"
  marker-lime-active: "#b9d72f"
  deep-marker: "#5f7a0c"
  marker-ink: "#12160a"
  bone: "#e8e6e1"
  bone-deep: "#4a4339"
  alarm: "#ff5f6d"
  alarm-deep: "#c8323f"
  darkroom: "#0b0c0e"
  darkroom-sunken: "#101113"
  darkroom-raised: "#17181a"
  darkroom-hover: "#1f2124"
  darkroom-selected: "#2a2c30"
  darkroom-control-border: "#323335"
  darkroom-hairline: "#1c1d1f"
  chrome-on-paper: "#14161a"
  print-white: "#f4f2ee"
  print-grey: "#c7ccd3"
  print-mid: "#a8aeb6"
  print-faint: "#8b8f96"
  print-disabled: "#47494d"
  proof-paper: "#edebe6"
  proof-paper-sunken: "#e3e0d9"
  proof-paper-raised: "#ffffff"
  paper-ink: "#14161a"
  paper-ink-secondary: "#3d434b"
  paper-ink-tertiary: "#5c6169"
  paper-ink-faint: "#71767e"
  paper-control-border: "#c9c5bc"
  paper-hairline: "#dcd8d0"
typography:
  display:
    fontFamily: "Instrument Sans, system-ui, sans-serif"
    fontSize: "44px"
    fontWeight: 500
    lineHeight: 1.14
    letterSpacing: "-0.02em"
  headline:
    fontFamily: "Instrument Sans, system-ui, sans-serif"
    fontSize: "1.375rem"
    fontWeight: 600
    lineHeight: 1.24
  title:
    fontFamily: "Instrument Sans, system-ui, sans-serif"
    fontSize: "1.0625rem"
    fontWeight: 500
    lineHeight: 1.32
  body:
    fontFamily: "Instrument Sans, system-ui, sans-serif"
    fontSize: "0.9375rem"
    fontWeight: 400
    lineHeight: 1.45
  reading:
    fontFamily: "Newsreader, Georgia, serif"
    fontSize: "21px"
    fontWeight: 300
    lineHeight: 1.66
  reading-heading:
    fontFamily: "Newsreader, Georgia, serif"
    fontSize: "29px"
    fontWeight: 400
    lineHeight: 1.25
  label:
    fontFamily: "IBM Plex Mono, monospace"
    fontSize: "0.8125rem"
    fontWeight: 500
    lineHeight: 1
    letterSpacing: "0.01em"
  label-regular:
    fontFamily: "IBM Plex Mono, monospace"
    fontSize: "0.8125rem"
    fontWeight: 400
    lineHeight: 1
rounded:
  hairline: "2px"
  xs: "4px"
  sm: "6px"
  control: "7px"
  md: "8px"
  cell: "10px"
  pill: "11px"
  sheet: "14px"
  dot: "50%"
spacing:
  cell-inset: "7px"
  phone-gap: "8px"
  phone-edge: "12px"
  desktop-gap: "16px"
  control-pad: "10px"
  header-pad: "20px"
  group-gap: "21px"
components:
  button-primary:
    backgroundColor: "{colors.marker-lime}"
    textColor: "{colors.marker-ink}"
    rounded: "{rounded.control}"
    padding: "0 15px"
    height: "34px"
  button-primary-hover:
    backgroundColor: "{colors.marker-lime-hover}"
  button-primary-active:
    backgroundColor: "{colors.marker-lime-active}"
  segmented:
    backgroundColor: "{colors.darkroom-raised}"
    textColor: "{colors.print-mid}"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    height: "30px"
    padding: "0 14px"
  segmented-active:
    backgroundColor: "{colors.marker-lime}"
    textColor: "{colors.marker-ink}"
  tag-chip:
    textColor: "{colors.print-mid}"
    typography: "{typography.label-regular}"
    rounded: "{rounded.pill}"
    height: "22px"
    padding: "0 9px"
  grid-cell:
    backgroundColor: "{colors.darkroom-raised}"
    rounded: "{rounded.cell}"
  cell-label:
    textColor: "{colors.print-grey}"
    typography: "{typography.label-regular}"
    rounded: "{rounded.xs}"
    padding: "4px 7px"
  action-sheet:
    rounded: "{rounded.sheet}"
    padding: "7px 16px 12px"
---

# Design System: Sema

## Overview

**Creative North Star: "The Contact Sheet"**

Sema is a photographer's proof sheet for the day's Feeds. Frames sit edge to edge on a justified grid, the important ones printed larger, and the user goes down the sheet with a grease pencil, marking what to keep and letting the rest go. Interest is told by Size and position, so the chrome never has to shout: it is the dark border of the sheet, printed in small mono type like the edge print on film, with one marker colour reserved for what needs a mark.

The system is dense and quiet. Images and titles carry the page; controls are small, mono-labelled and tightly grouped, and stay out of the way until a hand or key reaches for them. Reading happens in a serif, Newsreader, at a comfortable 640px measure. Light and dark themes share one structure: the page turns from Darkroom to Proof Paper, but the header, the Peek and every image surface stay dark, because they are the frame and the darkroom, not the paper.

Motion is short and mechanical (90–220ms), follows the input that caused it, and never decorates.

**Key Characteristics:**
- A justified grid of image and text cells, with three Sizes (small, medium, large) as the main hierarchy.
- Dark chrome and dark media surfaces in both themes.
- One accent, Marker Lime, spent only on state: unread, active, focus, the primary action.
- Three typefaces with fixed jobs: Instrument Sans for titles, IBM Plex Mono for metadata and chrome, Newsreader for reading.
- Flat at rest. Only Overlays, menus and toasts cast shadows.

## Colors

A near-black and warm-paper neutral pair, a bone white for Kept, and one acid-lime marker.

### Primary
- **Marker Lime** (`marker-lime`): the grease pencil. It fills the active segment in the header, the unread dot, primary buttons, focus rings and the scope count. Hover and pressed states step down to `marker-lime-hover` and `marker-lime-active`. Text set on it is always `marker-ink`.
- **Deep Marker** (`deep-marker`): Marker Lime's stand-in wherever the accent is text or a line on Proof Paper. Pure lime has no contrast on paper.

### Secondary
- **Bone** (`bone`): the Keep colour. It's used for the Kept marker, the Keep wash, the header Scope chip and the Play glyph. On Proof Paper it darkens to `bone-deep`.

### Tertiary
- **Alarm** (`alarm`): errors, and an Item about to Expire. On Proof Paper it deepens to `alarm-deep`. It's rare by design.

### Neutral
- **Darkroom** (`darkroom`): the dark page, and the chrome in the dark theme. Tonal steps lift from it for wells (`darkroom-sunken`), cells and controls (`darkroom-raised`), hover (`darkroom-hover`) and selection (`darkroom-selected`).
- **Chrome on Paper** (`chrome-on-paper`): the header and toolbars in the light theme. They stay dark over Proof Paper.
- **Print White → Print Disabled** (`print-white`, `print-grey`, `print-mid`, `print-faint`, `print-disabled`): a five-step text ladder on dark. White is for titles; grey and mid are for summaries and metadata; faint is for Read titles and quiet labels.
- **Proof Paper** (`proof-paper`): the light-theme page, warm off-white. Cells and sheets sit on `proof-paper-raised` (white), and wells use `proof-paper-sunken`.
- **Paper Ink** (`paper-ink` → `paper-ink-faint`): the same four-step text ladder on Proof Paper.
- **Rules**: rules are hairlines. On dark they are white at 5–18% alpha (the `--line-*` family), plus `darkroom-hairline` and `darkroom-control-border`. On paper they are `paper-hairline` and `paper-control-border`.

### Named Rules
**The Grease Pencil Rule.** Marker Lime marks state and nothing else: unread, active, focused, or the one primary action. If removing the lime would lose no information, it doesn't belong there.

**The Dark Frame Rule.** The header, the Peek, media scrims, cell labels over images and the Dock stay dark in both themes. Only the page and the surfaces that sit on it follow the theme.

## Typography

**Display Font:** Instrument Sans (with system-ui, sans-serif)
**Body Font:** Instrument Sans for interface copy; Newsreader (with Georgia, serif) for article bodies
**Label/Mono Font:** IBM Plex Mono (with monospace)

**Character:** A plain, slightly condensed grotesque does the talking, mono does the bookkeeping, and a quiet text serif takes over once the user settles in to read.

### Hierarchy
- **Display** (500, 44px, 1.14, −0.02em): the article title in the reader. Nowhere else.
- **Headline** (600, 1.375rem, 1.24): the Lead title of a Story cell.
- **Title** (500, 1.0625rem, 1.32): grid cell titles. They clamp to three lines, or two on L and spanning cells, which step up to 1.1875rem.
- **Body** (400, 0.9375rem, 1.45): cell summaries, settings copy and sheet rows.
- **Reading** (300 on dark / 400 on paper, 21px, 1.66): article bodies at a 640px measure. Quotes are set italic 300 at 22px with a 2px accent rule.
- **Reading Heading** (Newsreader 400, 29px, 1.25): headings inside article bodies. The same serif at 1.1875–2rem titles empty states and the end of the feed.
- **Label** (IBM Plex Mono 500/400, 0.8125rem, line-height 1, 0.01em): feed names, ages, counts, chips, the header's segmented controls and crumbs. All chrome text is label type.

### Named Rules
**The Edge Print Rule.** Anything that is bookkeeping is mono: a feed name, an age, a count, a Tag, a control label. Anything that is content is sans or serif. In the header, the scrolled reader title is the only sans text.

**The Three Faces Rule.** Instrument Sans, IBM Plex Mono and Newsreader cover every role. Don't add a fourth face or a new weight outside 300–600.

## Layout

The grid is a justified grid whose row geometry belongs to `web/src/layout/justified.ts`. A cell's Size (small, medium, large) sets its share of a row, so the grid itself is the ranking.

- **Desktop** (724px and wider): rows sit 16px apart, and Story Leads keep a 126px image floor. The header is a 56px shell with 20px edge padding, a 60px brand slot and a 2px bottom band.
- **Phone** (narrower than 724px, measured on the grid for the grid and on the viewport for chrome): rows sit 12px from each side with 8px gaps.
  - Every L Item is a full-width 168px row.
  - M and S Items pair two-up at 150px (124px for two S).
  - An L Story is a 200px Lead plus a 52px strip per shown Headline.
  - At 430px and below the grid header drops to 44px, with a 52px Tag strip under it that collapses on scroll.
- **Reader:** the article runs at a 640px measure. At 1200px and wider, the reader's identity aligns to that measure.
- **Controls** are 30px tall on desktop with 44px touch targets. Groups use 7px inner gaps and 21px outer gaps.
- **Safe areas:** every header adds the top safe-area inset above these heights.

The exact contracts, with their e2e coverage, are in `CODING_STANDARDS.md` under "UI contracts". That file is the authority for chrome geometry, and this section summarises it.

## Elevation & Depth

Surfaces lie flat on the sheet; only what floats above it casts a shadow. In the dark theme cells and controls have no shadow at all (`--shadow-raised: none`), and depth comes from the tonal steps from Darkroom up to Selected plus 1px hairlines. The light theme gives raised controls a single hairline shadow so white reads against Proof Paper. Real shadows are deep and soft, and belong only to things over the grid: menus, popovers and tooltips (6–10px offsets), toasts, the action sheet (−18px, upward), the Feeds drawer and Related panel (−18px, sideways), and dialogs (18–24px). Each comes with its own scrim, from 46% for sheets to 76% for modals.

### Shadow Vocabulary
- **Raised** (`--shadow-raised`): none on dark; `0 1px 2px` at 7% on paper. Used for segmented controls and raised controls.
- **Menu / Popover / Tooltip** (`--shadow-menu`, `--shadow-popover`, `--shadow-tooltip`): `0 6–10px 18–24px` at 45% black.
- **Sheet** (`--shadow-action-sheet`): `0 -18px 50px` at 42%. The phone action sheet rises from the bottom.
- **Drawer** (`--shadow-drawer`): `-18px 0 50px` at 45%. The Feeds drawer comes in from the right.
- **Dialog** (`--shadow-dialog`, `--shadow-confirm`): `0 18–24px 44–70px` at 55%.

### Named Rules
**The Flat Sheet Rule.** Nothing that lives in the grid or the header casts a shadow at rest. A shadow means the thing is floating above the sheet and will go away.

## Shapes

Corners are small and get bigger with the size of the object. Labels and age pills use 4–5px, controls 7px, the segmented track and media frames 8px, grid cells 10px, and the action sheet's top corners 14px. Tag chips are 11px capsules, and unread dots are circles. Images are clipped to their cell and covered edge to edge, with no inner padding. Every cell has a 1px hairline border.

## Components

### Buttons
Instrument-quiet: small, firm and rare.
- **Shape:** 7px radius (`rounded.control`), 34px tall (38px for "Open original" in the reader).
- **Primary:** Marker Lime fill, Marker Ink text, weight 600 at 0.8125rem, 0 15px padding. Use one per surface, for the action that moves the user on.
- **Hover / Focus:** hover steps to `marker-lime-hover` and pressed to `marker-lime-active`. Focus is a 2px ring with no outline: Print White around lime fills, Marker Lime around everything else.
- **Destructive:** quiet on purpose. A raised dark fill, a control border and lime text, rather than red, because removing a Feed is routine.
- **Disabled:** 38% opacity.

### Segmented controls
The header's main switches: Front Page or Chrono, and the Window (Today, Yesterday, All, plus unread only).
- **Style:** a 30px track on `darkroom-raised` with an 8px radius. Items are label-type mono with 14px side padding.
- **State:** the active item gets a Marker Lime fill inset 3px, with Marker Ink text. Inactive items are `print-mid` and turn Print White on hover.

### Chips
- **Tag chip:** a 22px capsule (11px radius) with a strong hairline border and mono 0.8125rem text, `print-mid`.
- **Header Scope chip:** a Bone fill with Marker Ink text. The active Scope is the one bone object in the header.
- **Cell labels** (age, Source count, rank): 4px radius on a dark 62% scrim, mono label type in `print-grey`. They stay dark in both themes.

### Grid cells
The signature component: the frames on the sheet.
- **Image cell:** the photo covers the cell. The title and feed name sit on a bottom scrim that fades from 95% black, and the age and unread dot sit top right. Every colour inside comes from the media tokens and stays dark in both themes.
- **Text-only cell:** a page-surface cell with the title at the top and a summary fitted to whole lines. It ends on one mono line holding the feed name, the age and the unread dot.
- **Story cell:** the Lead cell with its Headlines listed under it as single title lines, each with a Source badge. The Source count sits top left on L and at the end of the feed line on M.
- **Signal states:** Boost adds a lime wash and a top-left marker. Bury desaturates the media to 62% opacity and quietens the ink. Kept shows a Bone marker or chip.
- **Read:** the title drops to `print-faint`. Nothing else changes.
- **Hover actions:** 26px squares (24px on S cells) in a row from a 7px inset. On text cells they sit on a tray of the cell's own surface.

### Unread dot
A 7px Marker Lime circle. It enters in 100ms and leaves with a 140ms scale-and-fade when an Item is marked Read.

### Navigation
- **Header:** a 56px dark shell. The brand mark sits left, then the segmented switches; Filter, Search, Archive, Theme and Settings icons trail on the right (icons that navigate elsewhere trail; icons that act on the current Item lead). Icons are Lucide at 14–24px, with stroke 2–2.5 depending on size.
- **Phone:** a 44px translucent nav bar with a toolbar in the same material and 0.5px hairlines, and content scrolling under both.
- **Tag strip:** 52px, holding 36px chips, under the phone grid header.

### Overlays
- **Action sheet** (phone): rises from the bottom on the panel surface, with 14px top corners, a 34×4px grabber and 48px rows. Scrim at 46%.
- **Feeds drawer:** 420px wide, entering from the right, with a 46% scrim.
- **Confirm card:** 352px wide with a 10px radius on a raised panel.
- **Peek:** a media surface. Its bars, controls and caption stay dark in both themes.
- **Motion:** sheets and drawers enter in 180ms ease-out. The reader slides on `cubic-bezier(.2, .75, .2, 1)` at 200–220ms, and phone toolbars on `cubic-bezier(.32, .72, 0, 1)`. A surface opened by key appears in place without sliding (`web/src/input-modality.ts`).

## Do's and Don'ts

### Do:
- **Do** spend Marker Lime (#d6f24b) only on state: unread, active, focus, or the single primary action (The Grease Pencil Rule).
- **Do** switch accent text and lines to Deep Marker (#5f7a0c) on Proof Paper; lime stays a fill there.
- **Do** keep the header, the Peek, media scrims and cell labels over images dark in both themes (The Dark Frame Rule).
- **Do** set every bookkeeping string (feed name, age, count, Tag, control label) in IBM Plex Mono (The Edge Print Rule).
- **Do** take chrome heights, insets and the 724px phone edge from `CODING_STANDARDS.md`, and extend its e2e specs when a change touches a measured rule.
- **Do** gate motion on the input behind it: slide for pointer and touch, appear in place for keys.

### Don't:
- **Don't** set Marker Lime as text on Proof Paper or on white; it fails contrast.
- **Don't** give cells, the header or controls a shadow at rest (The Flat Sheet Rule).
- **Don't** add a fourth typeface or weights outside 300–600 (The Three Faces Rule).
- **Don't** show interest with badges, scores or colour; Size and position already show it.
- **Don't** let a top-band control (corner, markers, actions, Source count, Peek) cover a cell's copy or meet another control.
