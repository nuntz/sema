# Known Items are remembered forever in a marker separate from their identity

An Item gets one Live Window: a Republish is ignored even after the original Expires, whether or not it was Read. Before this, the `D#` identity row expired with the Item, so a feed that re-dated an entry, or left it undated so it took the fetch time, brought it back as a new unread Item once the Live Window ended.

`D#` can't carry this memory. Readers treat its expiry as "this Item is live", and a `D#` with no expiry already means a Kept Item pointing at its `A#` row. So a separate known marker, `KNOWN#<item-id>`, which never expires, is written in the same transaction as the Item, and dedupe tests that marker. `D#` keeps its meaning and its Live Window expiry. Unkeep, which deletes `D#` once the Live Window has passed, writes the marker in the same transaction, so a Kept Item never leaves without one.

## Considered Options

- **A fixed lifetime** such as 30 days after first ingest. Rejected: an undated entry that a feed keeps listing passes the age gate on every fetch, so it would come back after every lapse.
- **A sliding lifetime** renewed whenever a fetch still lists the entry. Rejected: the feed worker would write on every dedupe for as long as Sema runs, and an entry that dropped out of the feed for longer than the grace period would come back. Storage for markers that never expire is negligible at this scale.
- **Extending `D#` instead of adding a marker.** Rejected: every reader that treats `D#`'s expiry as liveness, plus Unkeep and identity reconciliation, would have to change meaning.

## Consequences

- Known markers stay when a Feed is removed and when an Item is Unkept. Re-adding a Feed shows only entries Sema does not know.
- Items written before this change have no marker. For 7 days after the deploy (2026-11-14 02:54 UTC), the feed worker's dedupe check wrote one when it found a live `D#` without a marker. This was a temporary, bounded exception to the rejection of writes while deduping, and it was removed on 2026-11-21 (#47): by then every non-Kept Item from before the change had Expired, and Kept ones get their marker at Unkeep. The fill could not cover an entry the dedupe check never saw during those 7 days, such as one in a feed that answers 304 Not Modified throughout, or one dated older than the Live Window until after its original Expires. Such an entry can still come back once. Dedupe still treats a live `D#` as known, which covers a Kept Item from before the change until it is Unkept.
- Rejected entries are not covered: they never became Items, and they are retried after their failure identity expires.
