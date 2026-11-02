# Read state is read-your-writes across API instances

The client sends a Read change, waits for the server to acknowledge it, and then loads counts and Items as a settled snapshot (`settledSnapshot` in `web/src/read-state.ts`). When it accepts that snapshot, it drops its optimistic adjustment. So every snapshot served after an acknowledged write must include that write, whichever API instance serves it.

Each API instance caches a user's `R#` markers for a minute to keep unread pages cheap. Lambda sends concurrent requests to separate instances, and an instance only patches its cache after writes it took itself. Counts served by another instance could therefore be up to a minute old, and a Tag chip read in its own Scope came back with its old count after the user hopped Scopes.

## Decision

A per-user `READ_VERSION` row counts Read changes. `SetRead` bumps it once per call, after its markers commit, and still bumps it when a later chunk fails, because earlier chunks have committed. The bump returns the new version.

Before reusing its cache, an instance reads `READ_VERSION` with a strongly consistent read. It reuses the cache only if the version matches the one it loaded at; otherwise it reloads the markers, also strongly consistent. The version is read before the markers, so a cache never carries a version newer than its contents. After its own write, an instance patches its cache forward only when the returned version is exactly one more than its cached version. Any other value means another instance wrote in between, and the cache is dropped.

## Consequences

- Every writer of `R#` markers bumps `READ_VERSION` after it commits. Today `SetRead` is the only writer.
- Each request that needs markers costs one small strongly consistent GetItem. A reload costs twice the read capacity of the old eventually consistent query.
- While old and new code overlap during a deploy, writes handled by old instances don't bump the version, so new instances can serve markers up to a minute old. This ends once all instances run the new code; no backfill is needed.

## Considered

- **Dropping the cache**: simplest and correct, but brings back the per-request marker query the cache was added to avoid.
- **A client "fresh" hint** that skips the cache after the client writes: cheapest, but every caller has to remember to send it, and anything that misses it reads stale markers.
