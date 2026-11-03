---
status: accepted
---

# Stories are read-your-writes across API instances

Each API instance caches rendered Stories for four minutes. Every rendered Member carries the user's Read, Boost, Bury and Keep state, and the client replaces its optimistic copy with whatever the next Story load returns (`reload` and `pollNew` in `web/src/item-session.ts`). Only the instance that took a write cleared its cache. So after a Read, Boost or Keep, a Story load served by another instance could show the change reverted: a read Story back in the unread grid, a Kept Item offering Bury (which the server then refuses), or a Boost undone.

## Decision

The Story cache key carries three per-user versions: `FEED_VERSION` (ADR 0006), `READ_VERSION` (ADR 0005) and a new `FEEDBACK_VERSION`. Each is read strongly consistent before rendering, so a cached list is never labelled newer than its contents.

Every Story list renders from the request's Read snapshot, whether unread only is on or off. The snapshot comes from the Read marker cache, labelled with the `READ_VERSION` read before its markers, and that version goes into the key. The list items request passes in its own snapshot, so its list and the Story Members it hides agree. Stories never resolve Read state from `R#` rows directly: that read is eventually consistent and could be older than the version in the key.

`FEEDBACK_VERSION` counts Boost, Bury, Keep and Unkeep. `SetSignal` and `SetHeart` bump it once after any call that attempted a write, including one that failed after attempting it, because it may have committed partway. A call rejected or failed before writing doesn't bump. A bump that fails after the write committed fails the request, as in ADR 0005.

## Consequences

- Every writer of Signal values or Keep state bumps `FEEDBACK_VERSION`. Today `SetSignal` and `SetHeart` are the only ones. Embedding updates on Signal rows don't change what Stories show, so they don't bump.
- Ranking order needed no new version: the model is already in the key, read strongly consistent, and a Signal updates it before returning.
- Each Story request costs one more small strongly consistent GetItem, for `FEEDBACK_VERSION`. Stories rendered with Read Items included now use the cached markers instead of a BatchGet of `R#` rows.
- Every Boost, Bury or Keep makes the user's other instances re-render their Stories, but leaves their Read marker caches warm.
- While old and new code overlap during a deploy, feedback written on old instances doesn't bump the version, so new instances can serve Stories up to four minutes old. A missing row reads as 0, so no backfill is needed.

## Considered

- **Accepting the four-minute TTL for feedback**: the client's reload overwrites its optimistic state, so a Boost or Keep visibly reverts and Bury is offered on a Kept Item.
- **Patching fresh Signal and Keep values in at serve time**: no version to bump, but it costs a Signal query and a Keep lookup on every request, most of what the cache saves.
- **Bumping `READ_VERSION` for feedback too**: one fewer read per Story request, but every Boost or Keep would also drop the Read marker cache.
- **Keying include-read Stories on `READ_VERSION` while still resolving `R#` rows**: smaller change, but the eventually consistent read could return markers older than the version it is cached under.
