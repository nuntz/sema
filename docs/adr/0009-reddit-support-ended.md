# Reddit support ended; Kept Reddit Items stay as they were

Reddit stopped serving RSS to everyone on 2026-11-13, and since 2026-11-17 every subreddit feed answers 404. Reddit had already closed logged-out `.json` and old.reddit.com, stopped accepting new requests for public Data API access after 2026-10-31, and ends remaining public API access in March 2027. No sanctioned path is left for an off-platform reader. See `docs/research/reddit-unauthenticated-access.md`.

Sema no longer reads Reddit. Reddit is not a Connector: Reddit Feeds can't be added, edited or imported, and the ones that existed are removed. Items Kept from Reddit stay in the Archive and still look and Send as they did, with the thread kept apart from the external link, their galleries, and their badge. That means `ConnectorReddit` and the Item-side Reddit rendering stay while ingestion goes.

## Considered Options

- **A registered app on Reddit's Data API with OAuth.** Rejected: Reddit stopped accepting new applicants after 2026-10-31 and ends remaining public access in March 2027. Even approved clients got JSON from `oauth.reddit.com`, not RSS.
- **A different User-Agent.** Rejected: the 404 is the RSS shutdown, not a refusal of this client. Refusals show up as 403 or 429.
- **Rendering Kept Reddit Items as generic Items.** Rejected: they would lose the split between the external link and the thread, which is why the user kept many of them.
- **Muting existing Reddit Feeds, or adding an "ended" Feed Status.** Rejected: muted rows would sit in Settings forever with nothing to unmute them for, and a Feed Status only one former Connector would ever use is not worth the vocabulary.

## Consequences

- Entering a Reddit URL (`r/name` or a reddit.com address) when adding a Feed, or importing one through OPML, gets a plain explanation that Reddit ended its public feeds. Only the small recognizer for Reddit URLs is kept, to catch them.
- Existing Reddit Feeds are removed by an idempotent backfill that dry-runs by default. Removing a Feed already keeps its Kept Items in the Archive, and Send already handles an Item whose Feed is gone. While a Feed exists, its Items borrow its connector, displayed title and favicon. So before removing each Feed, the backfill writes those onto the Feed's Kept Items. Otherwise Items ingested before the Reddit Connector existed would render as plain RSS, and custom titles would revert.
- The order is fixed. First, new Reddit Feeds are refused. Then the backfill runs in production. Only after that does the fetch code go, because the feed worker fails on a Feed whose Connector is not registered.
- `backfill-reddit-connector`, which turned subreddit RSS Feeds into Reddit Feeds, is deleted.
- Users get no notice in the app. Their Reddit Feeds leave Settings, and the backfill logs what it changed for each user.
- Replay no longer recovers missing gallery images from a Reddit post's own feed, because that feed no longer exists. Replay uses what is stored.
- Don't add a fetch path to Reddit back without new evidence that Reddit offers one, and don't delete the Reddit Item rendering while Kept Reddit Items exist.
