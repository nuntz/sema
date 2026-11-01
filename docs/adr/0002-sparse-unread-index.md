# Unread pages query a sparse index fed by a derived `unread_pk`

Read state lives in `R#` markers. Unread interest pages used to read Items in score order and throw away the ones already Read, which costs more as the share of Read Items grows. Now a live Item carries `unread_pk`, a derived copy of its unread status. Only unread Items have one, so only they appear in the sparse `unread-by-score` index, and unread pages query that index directly.

## Consistency

A marker and `unread_pk` change together in one transaction, conditioned on the live row existing. If they were separate writes, concurrent read and unread requests could interleave and leave an unread Item out of the index, where it would never show on unread pages.

The index is eventually consistent, so an Item just marked unread can be briefly missing from it. The UI keeps its optimistic state instead of reloading to confirm, because a reload could make the Item flicker out.

## Audit

A dry run of `cmd/backfill-unread-pk` audits index membership. With `--apply` it repairs any drift.
