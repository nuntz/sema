# Coding Standards

Rules a reviewer applies to a diff. Mechanical rules live in tooling (`make check`, `make e2e`); what is here needs judgement, or is a contract whose test covers only part of it.

## UI contracts

These hold for the grid and reader headers, the phone reader, and the lightbox. Rules marked _(e2e)_ are asserted by `web/e2e/header.e2e.ts`; check the rest by reading the diff, and extend that spec when a change touches a rule it doesn't cover.

### Desktop headers (620px and wider)

- Grid and reader headers sit on a 56px shell with 20px edge padding, a 60px brand slot, and a 2px bottom band. _(e2e: shell height, edge padding, band)_
- The reader read-progress gauge is 2px, inside the header band, centred on the article measure, and quiet by default. _(e2e: size and position)_
- At 1200px and wider, reader identity aligns to the 640px article measure; below 1200px the shim collapses. _(e2e: alignment, from 1280px)_

### Header text and icons

- The scrolled header title is the one sans-serif text in the header; all other header text stays mono. _(e2e)_
- Trailing icons navigate elsewhere; leading icons act on the current item.

### Grid header below 860px

- It carries the active Scope chip and a filter summary, with a 52px tag strip under it. The strip collapses on scroll; the header never does. _(e2e: the collapse)_
- At 430px and below, the grid header is 44px.

### Phone reader and lightbox

- The phone reader has a 44px translucent nav bar and a toolbar sharing one material, with 0.5px hairlines and content scrolling under both.
- The phone reader and lightbox bars add 16px below the top inset to clear the system edge blur.
- The phone reader toolbar sits 12px into the home-indicator inset.

### Safe areas and the installed iOS app

- Every header adds the top safe-area inset above the heights above. _(e2e)_
- In the installed iOS app, the root background matches the bar on top, because iOS tints the status bar from it. _(e2e: standalone tint)_

## Docs and decisions

- `RUNBOOK.md`, `README.md` and ADRs describe what is in the tree. A doc that cites a package, stack or job the diff doesn't contain is ahead of the code: hold it back, or mark the ADR proposed.
- A rule that changes in one doc changes everywhere it is stated; AGENTS.md, RUNBOOK.md and README.md must not contradict each other.

## Wizards

- In a `/wizard` script such as `scripts/setup-deploy.sh`, everything above the STAGES marker is the shared library: stages change, the library doesn't.
