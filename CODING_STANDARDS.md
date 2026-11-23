# Coding Standards

Rules a reviewer applies to a diff. Mechanical rules live in tooling (`make check`, `make e2e`); what is here needs judgement, or is a contract whose test covers only part of it.

## UI contracts

These hold for the grid and reader headers, the phone reader, the Peek, and grid cells. Rules marked _(e2e)_ are asserted by `web/e2e/header.e2e.ts` unless their section names another spec; check the rest by reading the diff, and extend that spec when a change touches a rule it doesn't cover.

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

### Grid cells

Front Page, Chrono, Scope and Archive cells share one anatomy (#48). Phone heights live in `web/src/layout/justified.ts`; _(e2e)_ here means `web/e2e/front-page.e2e.ts`.

- Phone rows sit 12px from each side with 8px gaps. Every L Item is a full-width 168px row; M and S Items sit two-up in equal widths, 150px when either is M and 124px for two S; a trailing single keeps half width. _(unit)_
- A phone Story is a full-width 200px Lead plus a 52px strip per shown Headline. `--story-strip-h` and `mobileStoryHeadlineHeight` state the same 52px. _(unit, e2e)_
- Desktop row geometry belongs to `justified.ts`: a cell change keeps its rows and the Story Lead's 126px image floor, so only measured copy grows a Story row. _(e2e: Story Lead floor)_
- An image cell is a media surface: the photo covers the cell, the title and feed name sit on the title scrim at the bottom, and the age and unread dot sit top right. Scrim, title and corner use media tokens in both themes. _(e2e)_
- A text-only cell (no `media_url`) starts its title at the top, fits its summary to whole lines through `web/src/ui/summary-fit.ts`, and ends on one line of feed name, age and unread dot. It uses page surface tokens. _(e2e)_
- A cell reserves its Signal why-line only while a Signal shows, on every cell type.
- A Boost or Bury shows as the top-left marker, plus the why-line on M and L cells; the corner holds only the age (or time-left marker) and the unread dot, and the cell's Open label speaks the Boost or Bury. A phone two-up cell, M or S, uses the compact time-left marker, and its image-cell title drops to two lines while that marker shows. _(e2e)_
- On a desktop text-only cell, the action row sits on a tray of the cell's own surface, so it covers the title's start only while visible. _(e2e)_
- The line under an Archive cell's title is the feed name alone, whatever Sema stored for the Item. _(e2e)_
- `?grid-images=off` is a memory experiment, not a reader setting: it keeps every cell's geometry, image boxes included. _(e2e: image-free grid)_

### Safe areas and the installed iOS app

- Every header adds the top safe-area inset above the heights above. _(e2e)_
- In the installed iOS app, the root background matches the bar on top, because iOS tints the status bar from it. _(e2e: standalone tint)_

## Web

- Every unread count shown while Reads are optimistic comes from `createUnreadCounts`' optimistic per-Feed counts; a count display never applies Read's adjustment itself, so the Scope cell, Tag chips and Scope picker can't disagree (#32).
- A change that must keep grid geometry is proven by a browser test measuring rendered rows, as in "desktop Story Leads keep master's image floor and Headline counts"; `justified.test.ts` injects measured heights, so it can't see a cell that reports a different one (#48).
- Behaviour that depends on the Window is tested in Today or Yesterday, with an Item fetched on a different day from the one it was published, as in `web/src/unread-counts.test.ts`: All holds every Item, and a same-day Item can't tell `fetched_ts` from `published_ts` (#46).

## Items

- Behaviour that changes when the Live Window ends (Republish, Read, Keep and Unkeep) is tested after the Item Expires as well as during its Live Window, by advancing the store's injected clock, as in `TestRepublishAfterTheLiveWindowIsIgnored`.

## API

- Each Lambda instance caches a user's Read markers, Feed lists and Stories, and concurrent requests land on different instances. That caching lives only in the Request snapshot (`cmd/api/internal/requestsnapshot`), which checks each cached kind against its version row (ADRs 0005–0007). New cached per-user state becomes another kind there, with no protocol code of its own. A store write to state a version guards (Read markers, user Feed edits, Signals, Keep state) runs inside `change` (`internal/store/version.go`), which bumps the version. A render the snapshot caches reads the state its versions cover strongly consistent, because an eventual read can return a value older than the version it is cached under; uncached handler reads may be eventual. Test a kind at the module's interface with two Caches over one DynamoDB Local table, aging entries only through the injected clock, as in `cmd/api/internal/requestsnapshot/read_test.go`, and add one two-server handler regression per bug, as in `cmd/api/read_instances_test.go`. A bug DynamoDB Local can't reproduce, such as a stale eventual read, instead gets a store test asserting `ConsistentRead` and a fake-store handler test asserting the render uses that read, as in `TestStoriesRenderFeedbackReadStrongly`.

## Flaky tests

- A change to how a test measures (garbage collection, waits, retries, settle loops, sampling) or to its bounds needs evidence in the PR that a deliberately injected bug still fails the test: the output of `scripts/expect-red.sh`, which fails unless the test ran and went red (#58). The plant breaks the behaviour the changed step guards, and its output shows the test failing at or after that step; a plant that fails an earlier assertion proves nothing about the change (#68). A change to when a script-driven animation's frames land, such as `animatePageScroll`'s clock, also needs the output of `scripts/load-compare.sh` against the code it replaces, because frame timing that passes one run at a time can fail on a busy machine (#63).
- A loop that waits for a value to settle fails with the readings it saw once it hits its cap.
- A test that resizes a rendered grid waits with `resizeGrid` (`web/e2e/grid-resize.ts`) before reading it, and a layout predicate fails on a detached or zero-size element, whose all-zero rects satisfy every `<=` (#57).
- A test that asserts a scroll lands exactly on a scroller's bottom, the grid's or the reader's, reads the extent with `settledScrollExtent` (`web/e2e/scroll-extent.ts`), since a height measured after the test's last wait, such as the grid's top slot, can still move it (#83).
- An e2e step proving something did not happen is followed by a step whose visible result would differ had it happened, including from every state the bug passes through; an assertion made straight after the action passes before the effect lands. It first polls for the trigger's visible result, so it can't pass on a run where the trigger never fired, and a plant that removes the trigger fails at that poll (#82: a remount test that never checked that the Lead's measurement had moved a row). For a video player, log its states with `logPlayerStates` (`web/e2e/youtube-stub.ts`, which says why) and check that none came before a later command's, such as a seek's buffering (3); when the app has no command to send, `seekPlayer` posts one from the page (#59, #60). A log proving a page made no requests is cleared only once the page before it has unloaded (`page.goto("about:blank")`), since a page being left still requests while the next one loads (#67). Fixed sleeps fail `bun run lint` (`web/biome-plugins/no-fixed-sleep.grit`).
- An element with an exit animation still matches its locator until it unmounts, so a check that it opened or stayed open asserts its open state, not its presence: the reader through `openReader` (`web/e2e/reader.ts`), which `bun run lint` enforces (`web/biome-plugins/no-closing-reader-visible.grit`). #69's test passed on the reader leaving after Escape, hiding for a hundred runs a click that opened nothing.
- A spec reads page state with `expect.poll(() => locator.evaluate(…))` rather than once; `bun run lint` rejects the inline `expect(await locator.evaluate(…))` form (`web/biome-plugins/no-one-shot-evaluate.grit`), but not a read stored in a variable first, so poll for the condition that makes such a value final. A poll that runs a side effect repeats it on each retry, so fire the effect once outside the poll. Before measuring something the app measures first, such as a Story row that grows when its Lead's height lands, wait for the app's marker (`data-lead-measured`, `data-layout-width`/`-height`) (#58).
- A settle poll observes the value it then checks, or waits with `animationsFinished` (`web/e2e/animations.ts`); a proxy that converges sooner, such as a 20px gap beside a 960px width, passes on an animation's tail. `SEMA_E2E_STRESS=animation` makes that fail on demand (#62).
- A test that passes after using more than half its timeout fails with its duration and timeout (`maxTimeoutShare`, `web/e2e/test.ts`), so a creeping test fails in every run rather than as a timeout on a busy machine. Make it faster or split it; raising its timeout hides the next creep (#65).

## Docs and decisions

- `RUNBOOK.md`, `README.md` and ADRs describe what is in the tree. A doc that cites a package, stack or job the diff doesn't contain is ahead of the code: hold it back, or mark the ADR proposed.
- A `CONTEXT.md` definition the diff relies on describes what the code does; when they disagree, the diff corrects whichever is wrong (#46).
- A new name for a domain concept uses its `CONTEXT.md` term rather than one of its _Avoid_ synonyms, and a concept the glossary lacks gets an entry in the same diff. Names that ship outside the code (an index, metric, alarm or log field) cost a migration to rename, so check those hardest; #74 nearly named an index `feed-health` against Feed Status's _Avoid: Health_.
- A rule that changes in one doc changes everywhere it is stated, including code comments that restate it; AGENTS.md, RUNBOOK.md and README.md must not contradict each other.

## Fixes

- A fix to code that is duplicated elsewhere fixes every copy in the same diff, or folds the copies into one: search beyond the diff for the pattern it fixes. The pattern is the code's shape, whatever element or module it reads: #83 fixed a one-shot `maxTop` read on the grid, and both reviewers passed over the same read on the reader as not the grid. `3237796` backed off throttled keys in one of three copied BatchGetItem loops, and the other two kept retrying at once until GET /stories returned a 500 (#73).

## Deletions

- A deletion also removes the struct fields, flags and branches that only the deleted code wrote: trace each field the deleted code set to its remaining writers, and delete the field, with whatever forwards it, when none is left. `deadcode` in `make check` finds unreachable functions, not fields that are still read but never written; #79 left `domain.Entry.PostType` and `ExternalURL` forwarded empty by the feed worker after the Reddit Connector went. The same goes for reads: an attribute in a `ProjectionExpression` that only the deleted code read goes too, since nothing checks the string; #47 left `item_sk` in `ItemKnown`'s identity projection.

## Backfills

- A backfill that deletes or rewrites rows names every reader of those rows and has a test per reader showing its output is the same after the run as before. For a Feed, that means the API's overlay (`domain.PresentWithFeed`), Send (`feedFields` in `internal/send/payload.go`), Search and ranking. Send's test is `TestItemPayloadOfAKeptItemIsUnchangedOnceItsFeedIsRemoved`. The #78 removal nearly dropped Send's `feed.url` and `tags`, which only the Feed row held (#75).

## Wizards

- In a `/wizard` script such as `scripts/setup-deploy.sh`, everything above the STAGES marker is the shared library: stages change, the library doesn't.
