---
status: accepted
---

# Feed edits are read-your-writes across API instances

After a user adds, edits, Mutes, retries, imports or deletes a Feed, the client waits for the acknowledgement and then reloads the Feed list. The next grid load also uses the change, because Scope and Mute decide which Feeds' Items appear. Each API instance caches the user's Feed list for a minute and Stories for four, but an instance only clears those caches after writes it took itself. So the next request, served by another instance, could show a Tag edit reverted, a deleted Feed back, a Muted Feed's Items or a renamed Feed's old title.

## Decision

A per-user `FEED_VERSION` row counts user-initiated Feed changes. Following ADR 0005, every instance reads it with a strongly consistent read before reusing the Feed list, the detailed Feed list or a cached Story list (the version is part of the Story cache key). If the version has moved, it reloads the Feeds strongly consistent. The writing instance drops its own caches rather than patching them forward: Feed writes are rare, and a reload is one Query.

Only the API's Feed writes bump the version: add, edit, retry, OPML import and delete. `cmd/feed-worker` writes every Feed after each fetch, and bumping there would empty every instance's cache constantly. So changes the worker or a backfill makes (Feed Status, fetched title, favicon, cadence) reach other instances only when the one-minute TTL expires. Nobody is waiting to read those back.

## Consequences

- The API writes Feeds only through store calls that bump the version and return it (`SaveFeeds`, `DeleteFeed`). `PutFeed`, which doesn't bump, is left to the worker and backfills, so a new API handler can't forget the bump.
- A request bumps once after its Feed writes commit, including an import that failed partway. A bump that fails after the write committed fails the request, and the client's retry is idempotent.
- `store.Feeds` always reads strongly consistent, so a reload after a bump can't return the old list.
- Each request that needs Feeds costs one small strongly consistent GetItem, next to the `READ_VERSION` read on paths that need both.
- While old and new code overlap during a deploy, Feed writes on old instances don't bump the version, so new instances can serve Feeds up to a minute old. This ends once all instances run the new code. A missing row reads as 0, so no backfill is needed.

## Considered

- **Dropping the Feed caches**: correct, but brings back a Feed Query on every grid, item, count and Story request.
- **Bumping on worker writes too**: catches fetched titles and favicons, but the constant fetches would leave the caches almost always stale.
- **Keying the Story cache on a hash of Feed presentation**: catches worker title and favicon changes too, but costs a hash of every Feed on each request.
- **Writing the Feed and bumping in one transaction**: atomic, but doubles write cost and doesn't fit the import's parallel writes.
