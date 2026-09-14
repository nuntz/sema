Large feed lists can now be narrowed with counted status and tag chips, searched, and sorted by unread or recent activity. Rows show all-time unread counts and identify feeds with no items in 30 days. A quiet triage banner exposes feeds whose estimated failure streak is at least seven days, with Show, sequential retry, and confirmed removal with an eight-second undo.

Filtering, counting, sorting, and failure-date estimation live in `web/src/feed-manager.ts`. App shares its existing counts refresh with the manager, restores window counts when returning to the grid, and renders action toasts in settings. Partial retry/removal failures are reported; undo restores only successfully removed feeds through the existing add-and-patch flow. The manager remains one list, with all 320 fixture feeds mounted.

Failure duration is an estimate from the last attempt and configured cadence. The existing retry API resets errors when it queues a worker, so “All feeds recovered” describes the refetched status, not a confirmed successful worker fetch. No Go API, DynamoDB schema, Pulumi configuration, or deployment changes are included.

Validation (from `web/`):

- `bun run lint`
- `bun run test` — 386 unit tests, including 20 feed-manager helper tests.
- `bunx tsc --noEmit`
- `bun run test:e2e e2e/feed-manager.e2e.ts e2e/settings-navigation.e2e.ts` — 32 browser tests covering keyboard navigation, search/filter composition, missing counts and sorts, sequential retry, confirmed removal, single/multiple undo, window-count restoration, and 320-feed desktop/phone layout.
