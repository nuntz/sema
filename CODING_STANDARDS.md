# Coding Standards

Rules a reviewer applies to a diff. Mechanical rules live in tooling (`make check`, `make e2e`); what is here needs judgement, or is a contract whose test covers only part of it.

## UI contracts

These hold for the grid and reader headers, the phone reader, and the Peek. Rules marked _(e2e)_ are asserted by `web/e2e/header.e2e.ts`; check the rest by reading the diff, and extend that spec when a change touches a rule it doesn't cover.

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
- The filter summary is the one raised control in the trailing group; Search and More are bare glyphs. _(e2e)_

### Phone reader and Peek

- The phone reader has a 44px translucent nav bar and a toolbar sharing one material, with 0.5px hairlines and content scrolling under both.
- The phone reader and Peek bars add 16px below the top inset to clear the system edge blur.
- Phone reader content starts 16px under the nav bar, the same air the bar leaves under the status blur. _(e2e)_
- The phone reader toolbar sits 12px into the home-indicator inset.
- The Peek is a media surface: its bars, controls and caption stay dark in both themes and at every width. _(e2e: `lightbox.e2e.ts`)_

### Safe areas and the installed iOS app

- Every header adds the top safe-area inset above the heights above. _(e2e)_
- In the installed iOS app, the root background matches the bar on top, because iOS tints the status bar from it. _(e2e: standalone tint)_

## Web

- Every unread count shown while Reads are optimistic comes from `createUnreadCounts`' optimistic per-Feed counts; a count display never applies Read's adjustment itself, so the Scope cell, Tag chips and Scope picker can't disagree (#32).
- Behaviour that depends on the Window is tested in Today or Yesterday, with an Item fetched on a different day from the one it was published, as in `web/src/unread-counts.test.ts`: All holds every Item, and a same-day Item can't tell `fetched_ts` from `published_ts` (#46).

## Items

- Behaviour that changes when the Live Window ends (Republish, Read, Keep and Unkeep) is tested after the Item Expires as well as during its Live Window, by advancing the store's injected clock, as in `TestRepublishAfterTheLiveWindowIsIgnored`.

## API

- Each Lambda instance caches a user's Read markers, Feed lists and Stories, and concurrent requests land on different instances. That caching lives only in the Request snapshot (`cmd/api/internal/requestsnapshot`), which checks each cached kind against its version row (ADRs 0005–0007). New cached per-user state becomes another kind there, with no protocol code of its own. A render the snapshot caches reads the state its versions cover strongly consistent, because an eventual read can return a value older than the version it is cached under; uncached handler reads may be eventual. Test a kind at the module's interface with two Caches over one DynamoDB Local table, aging entries only through the injected clock, as in `cmd/api/internal/requestsnapshot/read_test.go`, and add one two-server handler regression per bug, as in `cmd/api/read_instances_test.go`. A bug DynamoDB Local can't reproduce, such as a stale eventual read, instead gets a store test asserting `ConsistentRead` and a fake-store handler test asserting the render uses that read, as in `TestStoriesRenderFeedbackReadStrongly`.

## Flaky tests

- A change to how a test measures (garbage collection, waits, retries, settle loops, sampling) or to its bounds needs evidence in the PR that a deliberately injected bug still fails the test.
- A loop that waits for a value to settle fails with the readings it saw once it hits its cap.

## Docs and decisions

- `RUNBOOK.md`, `README.md` and ADRs describe what is in the tree. A doc that cites a package, stack or job the diff doesn't contain is ahead of the code: hold it back, or mark the ADR proposed.
- A `CONTEXT.md` definition the diff relies on describes what the code does; when they disagree, the diff corrects whichever is wrong (#46).
- A rule that changes in one doc changes everywhere it is stated; AGENTS.md, RUNBOOK.md and README.md must not contradict each other.

## Wizards

- In a `/wizard` script such as `scripts/setup-deploy.sh`, everything above the STAGES marker is the shared library: stages change, the library doesn't.
