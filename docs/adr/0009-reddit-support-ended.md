# Reddit support ended; Kept Reddit Items stay as they were

Reddit stopped serving RSS to everyone on 2026-11-13, and since 2026-11-17 every subreddit feed answers 404. Reddit had already closed logged-out `.json` and old.reddit.com, stopped accepting new requests for public Data API access after 2026-10-31, and ends remaining public API access in March 2027. No sanctioned path is left for an off-platform reader. See `docs/research/reddit-unauthenticated-access.md`.

Sema no longer reads Reddit. Reddit is not a Connector: Reddit Feeds can't be added or imported, and no Feed's URL or Reddit sort can be changed any more. The Reddit Feeds that existed are removed. Until then, a user can still edit what every Feed has, such as its title, Tags, Cadence and Mute, or Remove it. Items Kept from Reddit stay in the Archive and still look and Send as they did, with the thread kept apart from the external link, their galleries, and their badge. That means `ConnectorReddit` and the Item-side Reddit rendering stay while ingestion goes.

## Considered Options

- **A registered app on Reddit's Data API with OAuth.** Rejected: Reddit stopped accepting new applicants after 2026-10-31 and ends remaining public access in March 2027. Even approved clients got JSON from `oauth.reddit.com`, not RSS.
- **A different User-Agent.** Rejected: the 404 is the RSS shutdown, not a refusal of this client. Refusals show up as 403 or 429.
- **Rendering Kept Reddit Items as generic Items.** Rejected: they would lose the split between the external link and the thread, which is why the user kept many of them.
- **Muting existing Reddit Feeds, or adding an "ended" Feed Status.** Rejected: muted rows would sit in Settings forever with nothing to unmute them for, and a Feed Status only one former Connector would ever use is not worth the vocabulary.

## Consequences

- Entering a Reddit URL (`r/name` or a reddit.com address) when adding a Feed, or importing one through OPML, gets a plain explanation that Reddit ended its public feeds. A small recognizer for Reddit URLs catches them. Once the fetch code goes, it is the only Reddit Connector code kept.
- Existing Reddit Feeds are removed by an idempotent backfill that dry-runs by default. Removing a Feed already keeps its Kept Items in the Archive. While a Feed exists, its Items borrow its connector, displayed title and favicon, and Send also reads its URL and Tags. So before removing each Feed, the backfill writes those onto the Feed's Kept Items: the presentation fields, plus `feed_url` and `feed_tags`, which only this backfill writes and which Send reads only when the Feed is gone. Otherwise Items ingested before the Reddit Connector existed would render as plain RSS, custom titles would revert, and a Send would lose `feed.url` and `tags`.
- The order is fixed. First, new Reddit Feeds are refused. Then the backfill runs in production. Only after that does the fetch code go (#79), because the feed worker fails on a Feed whose Connector is not registered. Until then the Reddit fetch path, its scheduling and `backfill-reddit-connector` stay in the tree.
- The backfill removes each Feed with `store.DeleteFeed`, the same call a user's Remove uses, so it bumps `FEED_VERSION` and cached Feed lists refresh as after any Remove. This is an exception to ADR 0006, where backfills write Feeds with `PutFeed` and don't bump: a removed Feed lingering in another instance's cache for a minute would show a Feed the user no longer has.
- With the fetch code, `backfill-reddit-connector`, which turned subreddit RSS Feeds into Reddit Feeds, is to be deleted.
- Users get no notice in the app. Their Reddit Feeds leave Settings, and the backfill logs what it changed for each user.
- With the fetch code, replay is to stop recovering missing gallery images from a Reddit post's own feed, because that feed no longer exists, and use what is stored.
- Don't add a fetch path to Reddit back without new evidence that Reddit offers one, and don't delete the Reddit Item rendering while Kept Reddit Items exist.
