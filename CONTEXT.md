# Sema

A triage-first feed reader. Items arrive from subscribed feeds, live for a short window in a ranked grid, and disappear unless the user keeps them.

## Language

### Feeds

**Feed**:
One user's subscription to a source, together with their settings for it: tags, custom title, and mute state. Every Feed belongs to exactly one user, even when two users follow the same source.
_Avoid_: Subscription, source, channel, subreddit

**Connector**:
The kind of source a Feed reads: RSS (covering Atom and JSON Feed), YouTube, or Reddit.
_Avoid_: Transport, provider, adapter, type

**Tag**:
A user-chosen label on a Feed. Tag views show only Items from Feeds carrying that Tag, and each Tag has its own Size cutoffs.
_Avoid_: Folder, category, group, label

**Mute**:
Stopping a Feed without removing it. A muted Feed is not fetched and its live Items disappear from every view until it is unmuted.
_Avoid_: Pause, disable, snooze, hide

**Feed Status**:
How a Feed is doing: ok, slowed after a fetch error, broken after repeated errors or after a day of being Refused, or muted. A Feed broken by refusals keeps being checked at its Cadence.
_Avoid_: Health, state

**Refused**:
A fetch the source turned away because of who is asking or how often, not because the Feed is gone. A Refused fetch never slows a Feed and is simply tried again; only a Feed refused continuously for a day becomes broken.
_Avoid_: Rate limited, throttled, blocked, error

**Cadence**:
How often Sema checks a Feed for new Items: hourly, every three or six hours, or daily. Cadence follows how often the Feed actually publishes unless the user pins it.
_Avoid_: Interval, poll rate, frequency, schedule

**Link Item**:
An Item that points at something Sema is not meant to extract, so having no body is normal and not a failure. A Reddit or Bluesky post without an external link is a Link Item, and so is every Item of a Link Feed.
_Avoid_: Pointer, stub, headline-only item

**Link Feed**:
A Feed whose Items are all Link Items, judged from its recent history when the source cannot say per Item, such as an RSS feed of image posts. A Feed leaves the class when it starts carrying articles again.
_Avoid_: Aggregator, no-body feed, link aggregator

### Items

**Item**:
One post from one Feed. The same article arriving through two Feeds is two Items, which then form a Story.
_Avoid_: Entry, article, post, card

**Live Window**:
The seven days after publication during which an Item stays in the grid. An Item fetched late gets only what is left, and one published more than seven days ago never enters. Anything not kept by the end of the Live Window is gone.
_Avoid_: Retention period, queue, TTL

**Expire**:
What happens to an unkept Item at the end of its Live Window: it leaves the grid and is deleted. Removing a Feed makes its live Items vanish at once, while its Kept Items stay in the Archive.
_Avoid_: Purge, clean up, delete (as a user action)

**Republish**:
A Feed serving an Item that Sema has already seen. A republished Item is ignored while the original is live or kept, so a Kept Item never returns to the grid.
_Avoid_: Duplicate, re-ingest, resurface

### Keeping

**Keep**:
The user action that makes an Item permanent. Keeping an Item always counts as a Boost; a Kept Item can never be Buried or neutral.
_Avoid_: Heart, favourite, save, star, archive (as a verb)

**Unkeep**:
The reverse of Keep. Unkeeping returns the Item to neutral feedback regardless of any Boost made before it was kept.
_Avoid_: Unheart, remove from archive

**Kept Item**:
An Item the user has kept. It survives the Live Window and cannot re-enter the grid if its feed republishes it.
_Avoid_: Hearted item, archived item, favourite

**Archive**:
The user's collection of Kept Items. Only a noun: the place Kept Items live, never the action of putting them there.
_Avoid_: Favourites, saved items, library

### Triage

**Read**:
The state of an Item the user is done with, whether they opened it or paged past it. Read never influences ranking and lapses with the Live Window.
_Avoid_: Seen, cleared, dismissed, skipped

### Grid

**Front Page**:
The live grid in interest order, where Stories and single Items interleave by rank. Chrono order, tag views, and the Archive are plain Item lists and never contain Stories.
_Avoid_: Feed, timeline, home, grid (when Stories are meant)

**Size**:
The visual weight of a cell on the grid: small, medium, or large, cut at the 60th and 90th percentile of the user's scores. A Story is never small.
_Avoid_: Tier, rank, priority

**Cluster**:
A group of live Items that cover the same thing, joined by embedding similarity or a shared URL. An Item joins a Cluster when it is close enough to any existing Member published within 72 hours of it, so a Cluster can outlive that window by chaining.
_Avoid_: Story (until it has two Sources), group, thread, dedupe set

**Story**:
A Cluster with two or more Sources. Only Stories appear on the Front Page; a single-Source Cluster shows as separate Items.
_Avoid_: Cluster (once it qualifies), topic, event, bundle

**Member**:
An Item that belongs to a Cluster. Marking a Story read marks every Member.
_Avoid_: Child, entry, duplicate

**Lead**:
The Member that stands for its Story: highest score, then media, then extraction quality. Boost, Bury, Keep, and Open on a Story act on the Lead only.
_Avoid_: Primary, representative, head, founder

**Headline**:
A Member other than the Lead, shown under it as a title line.
_Avoid_: Sibling, related, secondary

**Source**:
A distinct feed among a Cluster's Members. Source count decides whether a Cluster is a Story and lifts a Story's rank.
_Avoid_: Feed (in this sense), outlet, publisher

**Cursor**:
The Item or Story the grid's keys act on. Hovering a cell moves the Cursor there.
_Avoid_: Focus, selection, highlight, active cell

### Overlays

**Overlay**:
A surface opened over the grid or reader that owns the keyboard and the back button while it is on top. An Overlay closes with Escape and with the key that opened it, except an Overlay holding a text input, which closes with Escape only.
_Avoid_: Modal, dialog, sheet, popup, panel

**Peek**:
An Overlay that shows an Item's images over the grid without marking the Item Read. A Peek on a Story shows the Lead's images.
_Avoid_: Lightbox, preview, image viewer, gallery

**Flip**:
Leaving a Peek for the reader of the same Item.
_Avoid_: Open, expand, go to article

### Feedback

**Feedback**:
The user's current Keep, Boost, or Bury choice for an Item. Keeping always implies a Boost; Unkeep returns the Item to neutral. Story feedback acts on its Lead only.
_Avoid_: Rating, vote, reaction

**Signal**:
Explicit feedback the user gives on an Item: a Boost, a Bury, or a Keep. A Signal on an Item always outweighs any Behaviour on the same Item.
_Avoid_: Explicit feedback, vote, rating

**Behaviour**:
Implicit feedback inferred from what the user does with an Item: opening it, dwelling on it, clicking through, or sharing. Behaviour only ever counts in an Item's favour.
_Avoid_: Events, reading behaviour, implicit feedback, engagement

**Boost**:
Explicit feedback that the user wants more Items like this one. A Kept Item is always Boosted.
_Avoid_: Like, upvote, heart, +1

**Bury**:
Explicit feedback that the user wants fewer Items like this one. Never applies to a Kept Item.
_Avoid_: Dislike, downvote, hide, -1
