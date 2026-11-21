# Reddit: unauthenticated feed access and the sanctioned alternatives

Research date: 2026-11-21

Question: has Reddit stopped serving logged-out RSS/Atom (`/r/<sub>/.rss`), `.json` listings, and old.reddit.com equivalents? If so, what changed and when, and what sanctioned path is left for a small, personal, non-commercial feed reader?

How to read the citations:

- **Reddit** means a Reddit-owned page (support.reddithelp.com, reddit.com, redditinc.com, Reddit's GitHub wiki).
- **Reddit (excerpt)** means a Reddit Help Center page known only from the search index's excerpt of it. Reddit blocks this research tool's crawler: fetching reddit.com, redditinc.com and developers.reddit.com fails outright, and support.reddithelp.com answers a direct fetch with 403. So those pages could not be read in full, and the excerpts may leave out qualifiers.
- **Secondary** means news, blogs or vendor pages. They are used only where no readable Reddit source exists, and are labelled as such.

## Answer

- **Reddit has ended RSS for everyone, not just for some User-Agents.** Reddit's Help Center changelogs say "Reddit will stop supporting RSS feeds" on 2026-11-13 and that "if you use RSS for feeds outside of a community you moderate, there is no replacement" ([Changelog, 2026-10-08](https://support.reddithelp.com/hc/en-us/articles/54353370049684-Changelog-October-8-2026); [Changelog, 2026-11-10](https://support.reddithelp.com/hc/en-us/articles/55472297340308-Changelog-November-10-2026); Reddit (excerpt)). A feed reader vendor reports that subreddit, user and comment feeds began answering "Not Found" on 2026-11-17 ([Feeder, updated 2026-11-21](https://feeder.co/knowledge-base/rss-feed-creation/reddit-rss/); Secondary). That matches a 404 on every fetch from 2026-11-17 onward, from both cloud and residential IPs.
- **Unauthenticated `.json` and logged-out old.reddit.com were closed before RSS.** Reddit's r/modnews post of 2026-05-28 reportedly said "We'll also be shutting down unauthenticated `.json` endpoints" and that "logged-in and authenticated access won't be impacted". Old Reddit now requires login. Reddit's own post could not be read, so these quotes are known only through secondary sources (see §1).
- **Registering a Data API app (option a) is closed to new applicants and ends for everyone in 2027.** Reddit's changelogs say "new requests for public API access will no longer be accepted" after 2026-10-31. Access removal begins on 2027-01-12 for apps and users that have not registered or responded to Reddit's outreach, and "remaining public API access ends" in March 2027 ([Changelog, 2026-10-08](https://support.reddithelp.com/hc/en-us/articles/54353370049684-Changelog-October-8-2026); [Changelog, 2026-11-10](https://support.reddithelp.com/hc/en-us/articles/55472297340308-Changelog-November-10-2026); Reddit (excerpt)).
- **No sanctioned path is left for an off-platform personal reader.** Reddit points builders to its on-platform Developer Platform ("Devvit") ([Responsible Builder Policy](https://support.reddithelp.com/hc/en-us/articles/42728983564564-Responsible-Builder-Policy); Reddit (excerpt)). Its only RSS replacement is a moderator alert relay to Discord or Slack ([Setting up mod alerts in Discord or Slack](https://support.reddithelp.com/hc/en-us/articles/54058188280852-Setting-up-mod-alerts-in-Discord-or-Slack); Reddit (excerpt)). Commercial use needs a written agreement.
- **While it lasted, the OAuth path was JSON at `oauth.reddit.com`, never RSS.** Reddit's documentation covers only JSON at `oauth.reddit.com` with a bearer token, and RSS no longer exists in any form. The terms also require deleted content to be purged from any store ([Data API Wiki](https://support.reddithelp.com/hc/en-us/articles/16160319875092-Reddit-Data-API-Wiki); Reddit (excerpt)).

## 1. Unauthenticated RSS, `.json` and old.reddit.com: what changed and when

### RSS (confirmed by Reddit)

- Reddit announced the end of RSS on 2026-09-30 in r/modnews ("Continuing our infrastructure updates", u/judy-funnie). The post's URL is `https://www.reddit.com/r/modnews/comments/1wubgvt/continuing_our_infrastructure_updates_whats/`, but it could not be read. Its wording comes through secondary quotation: "Because RSS is now a common surface for large-scale scraping and automated abuse, we'll stop supporting RSS feeds on November 13, 2026 while preserving moderation workflows that depend on it through supported alternatives" ([Michael Tsai, 2026-10-06](https://mjtsai.com/blog/2026/10/06/reddit-to-remove-rss/); [TechCrunch, 2026-09-30](https://techcrunch.com/2026/09/30/reddit-is-killing-rss-feeds-ending-public-api-access-because-of-ai-bots/); Secondary).
- Reddit's own Help Center repeats the date and the absence of a replacement: "Reddit will stop supporting RSS feeds" on 2026-11-13, and "if you use RSS for feeds outside of a community you moderate, there is no replacement" ([Changelog, 2026-10-08](https://support.reddithelp.com/hc/en-us/articles/54353370049684-Changelog-October-8-2026); [Changelog, 2026-11-10](https://support.reddithelp.com/hc/en-us/articles/55472297340308-Changelog-November-10-2026); Reddit (excerpt)).
- The only replacement Reddit names is for moderators: the Discord Relay Devvit app, which sends a community's mod alerts to Discord or Slack ([Setting up mod alerts in Discord or Slack](https://support.reddithelp.com/hc/en-us/articles/54058188280852-Setting-up-mod-alerts-in-Discord-or-Slack); Reddit (excerpt)).
- Reddit had flagged RSS earlier. Its r/modnews post "Protecting communities from scrapers and platform abuse" (`/r/modnews/comments/1tq9vxo/`, about late May or June 2026, u/boat-botany) reportedly called RSS "another common surface for scraping" and asked moderators "how and for what purpose" they used feeds ([PPC Land](https://ppc.land/reddit-to-halt-public-data-api-access-in-march-2027-as-14-000-apps-register/); Secondary).
- Unrelated to readers: Reddit Pro's publisher dashboard still *ingests* a publisher's own RSS feed. That is Reddit consuming RSS, not serving it ([Reddit Pro Feature: Content](https://support.reddithelp.com/hc/en-us/articles/47619289307412-Reddit-Pro-Feature-Content); Reddit (excerpt)).

### What the shutdown looks like in practice (secondary only)

Reddit has not documented which status code a retired feed returns. Everything in this subsection comes from third-party observation:

- Feeder (a feed-reader vendor) says search feeds stopped first, around 2026-11-10. Subreddit, new-posts, comment, multi-subreddit and user feeds followed on 2026-11-17 and now answer "Not Found". Search feeds answer "forbidden", and old.reddit.com feeds show "You've been blocked by network security" (rechecked 2026-11-21) ([Feeder](https://feeder.co/knowledge-base/rss-feed-creation/reddit-rss/); Secondary).
- A self-hosting newsletter says the feeds "finally stopped working this past Monday" (issue dated 2026-11-20) ([Self-Host Weekly, 2026-11-20](https://selfh.st/weekly/2026-11-20); Secondary).
- Before the shutdown, logged-out feeds degraded in stages. A third-party error guide reports 429s after the logged-out feed limit fell to about one request per minute (2026-06-11), and 403s for requests with no User-Agent. Since about 2026-10-13, some cloud clients got 403 "You've been blocked by network security" on `.json` and API calls ([Hushread, Oct 2026](https://hushread.com/blog/reddit-rss-not-working); Secondary).

**Inference (not confirmed by Reddit):** a 404 that started on 2026-11-17 on `/r/<sub>/.rss`, and that is the same from cloud and residential IPs, fits the scheduled RSS retirement. It does not fit a User-Agent rejection, which shows up as 403 or 429 in the reports above, or IP reputation. Changing the User-Agent would not bring the feeds back.

### Unauthenticated `.json` (Reddit statement, secondary quotation)

- The 2026-05-28 r/modnews post reportedly said: "We'll also be shutting down unauthenticated `.json` endpoints. These endpoints can be used to scrape Reddit without accountability… Logged-in and authenticated access won't be impacted." A developer write-up says such requests began returning 403 within days ([DEV Community](https://dev.to/tonywangca/why-reddit-blocked-unauthenticated-json-in-2026-and-how-to-still-get-reddit-data-58b9); Secondary). Another source quotes the same "without accountability" phrase ([PPC Land](https://ppc.land/reddit-to-halt-public-data-api-access-in-march-2027-as-14-000-apps-register/); Secondary).
- Reddit's long-standing rule already pointed this way: "Traffic not using OAuth or login credentials will be blocked, and the default rate limit will not apply" ([Data API Wiki](https://support.reddithelp.com/hc/en-us/articles/16160319875092-Reddit-Data-API-Wiki); Reddit (excerpt)). Access from a hosted provider's IP range likewise requires "a valid OAuth token or be logged in" ([Developer Platform & Accessing Reddit Data](https://support.reddithelp.com/hc/en-us/articles/14945211791892-Developer-Platform-Accessing-Reddit-Data); Reddit (excerpt)).

### old.reddit.com

- The 2026-09-30 r/modnews post says Old Reddit's "largest source of abusive scraping and automated traffic" is "unauthenticated access". Reddit will "further limit access for logged-in users to only those who have used Old Reddit in the last six months", and logged-in moderators are exempt ([Michael Tsai, 2026-10-06](https://mjtsai.com/blog/2026/10/06/reddit-to-remove-rss/); [PPC Land](https://ppc.land/reddit-to-halt-public-data-api-access-in-march-2027-as-14-000-apps-register/); Secondary quotation of a Reddit post).
- Third parties report that logged-out old.reddit.com requests have redirected to `/login/` since mid-2026 ([Hushread](https://hushread.com/blog/reddit-rss-not-working); Secondary).

## 2. User-Agent requirements Reddit documents

- Required format: `<platform>:<app ID>:<version string> (by /u/<reddit username>)`, for example `android:com.example.myredditapp:v1.2.3 (by /u/kemitche)`. Generic agents such as "Python/urllib" or "Java" are "drastically limited", and Reddit says "NEVER lie about your user-agent" ([reddit-archive API wiki, last edited 2015-12-18](https://github.com/reddit-archive/reddit/wiki/API); Reddit, archived).
- The current Help Center wiki repeats the format and adds "We can and will freely throttle or block unidentified Data API users" ([Data API Wiki](https://support.reddithelp.com/hc/en-us/articles/16160319875092-Reddit-Data-API-Wiki); Reddit (excerpt)).
- A User-Agent of the form `linux:<app>:rss` has no version string and no `(by /u/…)` contact, so it does not follow the documented format. Reddit documents no 404 for a bad User-Agent, though, and fixing it would not revive RSS (see §1).

## 3. The OAuth path: app types, approval, limits, cost

### Mechanics (Reddit, archived docs)

- App types: **web** (a server that can keep a secret), **installed** (a device that cannot keep a secret, so no client secret) and **script** (runs on hardware you control, with access only to the developer's own account) ([reddit-archive OAuth2 wiki, last edited 2016-01-08](https://github.com/reddit-archive/reddit/wiki/OAuth2); Reddit, archived).
- Application-only ("userless") OAuth: the `client_credentials` grant for confidential clients, or `https://oauth.reddit.com/grants/installed_client` with a `device_id` for installed apps. Tokens come from `POST https://www.reddit.com/api/v1/access_token` with HTTP Basic auth and carry no refresh token. API calls then go to `oauth.reddit.com` with `Authorization: bearer <token>` (same source).

### Approval (Reddit)

- Since the Responsible Builder Policy (announced in r/redditdev on 2025-11-11, "Introducing the Responsible Builder Policy…"), you "must request access and get explicit approval before accessing any Reddit data through our API". Multiple accounts or repeat requests for the same use case are prohibited, and "developers should use the Developer Platform ('Devvit') to build apps on Reddit" ([Responsible Builder Policy](https://support.reddithelp.com/hc/en-us/articles/42728983564564-Responsible-Builder-Policy); Reddit (excerpt)). The request form is to be used "only if your bot/app is not supported by Devvit" ([Developer Platform & Accessing Reddit Data](https://support.reddithelp.com/hc/en-us/articles/14945211791892-Developer-Platform-Accessing-Reddit-Data); Reddit (excerpt)).
- Commercial use requires "explicit written approval". Non-commercial use has a separate sign-up path (same page; Reddit (excerpt)).

### The 2026–2027 wind-down (Reddit)

| Date | Change | Source |
| --- | --- | --- |
| 2026-10-31 | New requests for public API access no longer accepted | [Changelog, 2026-10-08](https://support.reddithelp.com/hc/en-us/articles/54353370049684-Changelog-October-8-2026) (excerpt) |
| 2026-11-13 | RSS support ends | same; [Changelog, 2026-11-10](https://support.reddithelp.com/hc/en-us/articles/55472297340308-Changelog-November-10-2026) (excerpt) |
| 2026-12-31 | App Migration Program ends (a $1,000 payout to selected *existing* apps that serve communities with 1,000+ weekly actives or provide important end-user tooling, and that predate 2026-03-25) | [App Migration Program 2026 Terms](https://support.reddithelp.com/hc/en-us/articles/47822311698452-Reddit-Developer-Platform-App-Migration-Program-2026-Terms) (excerpt) |
| 2027-01-12 | Access removal begins for apps and users that have not registered or responded to outreach | both changelogs (excerpt) |
| March 2027 | "Remaining public API access ends" | both changelogs (excerpt) |

Reddit frames the change as "moving public Data API apps and bots to the Developer Platform (Devvit)" to reduce scraping and automated abuse ([Changelog, 2026-11-10](https://support.reddithelp.com/hc/en-us/articles/55472297340308-Changelog-November-10-2026); Reddit (excerpt)). An admin reportedly said Reddit will "have exemptions for partners with written agreements in place" ([PPC Land](https://ppc.land/reddit-to-halt-public-data-api-access-in-march-2027-as-14-000-apps-register/); Secondary quotation).

### Rate limits and cost (Reddit)

- Free OAuth limit: 100 queries per minute per OAuth client ID, averaged over a window (currently 10 minutes), reported in the `X-Ratelimit-Used`, `X-Ratelimit-Remaining` and `X-Ratelimit-Reset` headers ([Data API Wiki](https://support.reddithelp.com/hc/en-us/articles/16160319875092-Reddit-Data-API-Wiki); Reddit (excerpt)).
- Non-commercial use was free under that limit, and commercial use was priced separately. That free tier is now closed to new requests and ends in March 2027 (table above).

**Net for a personal reader:** a new Data API app would need to have been requested before 2026-10-31, approved under the Responsible Builder Policy, and registered before 2027-01-12, and it would still lose access in March 2027. Reddit's alternative, Devvit, runs apps on Reddit, not as an external client pulling listings.

## 4. Can OAuth clients still get RSS?

- No Reddit document describes RSS on `oauth.reddit.com`. The documented OAuth surface is the JSON API there ([reddit-archive OAuth2 wiki](https://github.com/reddit-archive/reddit/wiki/OAuth2); Reddit, archived).
- Reddit says it "will stop supporting RSS feeds", with no carve-out for authenticated users ([Changelog, 2026-10-08](https://support.reddithelp.com/hc/en-us/articles/54353370049684-Changelog-October-8-2026); Reddit (excerpt)). Before the shutdown, logged-in "private" feed URLs with `user=` and `feed=` tokens from `reddit.com/prefs/feeds` reportedly got around the logged-out limits ([Hushread](https://hushread.com/blog/reddit-rss-not-working); Secondary). No source says whether they survived 2026-11-13, and they are tied to one account's credentials.
- Conclusion: until March 2027, an approved OAuth client must use the JSON listings (for example `GET https://oauth.reddit.com/r/<sub>/new`), not RSS.

## 5. Terms affecting storing and displaying content and images

- Deleted content: developers "must remove any user content in their possession that has been deleted from Reddit". On account deletion they must strip the user ID and author-identifying fields. Reddit "strongly recommends" deleting stored user data and content within 48 hours, and retaining deleted content "even if disassociated, de-identified or anonymized" violates its terms ([Data API Wiki](https://support.reddithelp.com/hc/en-us/articles/16160319875092-Reddit-Data-API-Wiki); Reddit (excerpt)). A stored-and-indexed copy of posts would need a periodic re-check-and-purge loop.
- Display: the licence is to copy and display User Content to end users, and changes are limited to formatting it for display. When access ends, apps must stop using the APIs and delete cached or stored content (Help Center search excerpt; the governing text is the Data API Terms at redditinc.com, which could not be read; see Unknowns).
- AI and model training: content may not be used "as an input for any model training without explicit consent from Reddit" ([Developer Platform & Accessing Reddit Data](https://support.reddithelp.com/hc/en-us/articles/14945211791892-Developer-Platform-Accessing-Reddit-Data); Reddit (excerpt)). This matters if posts would feed an embedding or ranking model.
- Scraping without an authorised agreement is prohibited site-wide ([Don't break the site](https://support.reddithelp.com/hc/en-us/articles/360043512931-Don-t-break-the-site); Reddit (excerpt)). Scraping HTML is therefore not a sanctioned substitute.

## Unknowns

- **Full primary texts.** The 2026-05-28 and 2026-09-30 r/modnews posts, the 2026-09-30 r/redditdev post ("Moving Data API apps to the Developer Platform"), the Data API Terms, the Developer Terms, developers.reddit.com and Reddit's robots.txt could not be read, because Reddit blocks this tool's crawler. Help Center pages are known only from search-index excerpts. Reddit's wording is quoted from secondary sources wherever the Reddit page could not be read.
- **Status code.** Reddit has not documented the 404 on retired feeds, and the exact cutover time per feed type is not documented either. The 2026-11-17 date for subreddit feeds is secondary.
- **Whether "public API access ends" in March 2027 covers every OAuth client.** That would include an already-approved personal script. The phrase is Reddit's, but no Reddit text that could be read defines its scope or any personal-use exception beyond partner written agreements (Secondary).
- **Whether Devvit can serve an external reader.** No readable Reddit document says whether a Devvit app can export subreddit listings to an off-platform client. Devvit's own documentation lives on developers.reddit.com, which could not be read.
- **Whether logged-in private RSS feed tokens still work after 2026-11-13.** Not documented.
- **The exact Data API Terms clauses** on caching, media and images, and attribution.

## Sources

Reddit-owned (Help Center pages read via search excerpts; dates as titled or noted):

- [Changelog – October 8, 2026](https://support.reddithelp.com/hc/en-us/articles/54353370049684-Changelog-October-8-2026) (2026-10-08)
- [Changelog – November 10, 2026](https://support.reddithelp.com/hc/en-us/articles/55472297340308-Changelog-November-10-2026) (2026-11-10)
- [Reddit Data API Wiki](https://support.reddithelp.com/hc/en-us/articles/16160319875092-Reddit-Data-API-Wiki) (undated; last update roughly mid-2026 per index)
- [Developer Platform & Accessing Reddit Data](https://support.reddithelp.com/hc/en-us/articles/14945211791892-Developer-Platform-Accessing-Reddit-Data) (undated)
- [Responsible Builder Policy](https://support.reddithelp.com/hc/en-us/articles/42728983564564-Responsible-Builder-Policy) (policy announced 2025-11-11)
- [Reddit Developer Platform App Migration Program 2026 Terms](https://support.reddithelp.com/hc/en-us/articles/47822311698452-Reddit-Developer-Platform-App-Migration-Program-2026-Terms) (term 2026-03-31 to 2026-12-31)
- [Setting up mod alerts in Discord or Slack](https://support.reddithelp.com/hc/en-us/articles/54058188280852-Setting-up-mod-alerts-in-Discord-or-Slack) (undated, 2026)
- [Reddit Pro Feature: Content](https://support.reddithelp.com/hc/en-us/articles/47619289307412-Reddit-Pro-Feature-Content) (undated)
- [Don't break the site](https://support.reddithelp.com/hc/en-us/articles/360043512931-Don-t-break-the-site) (undated)
- [reddit-archive wiki: API](https://github.com/reddit-archive/reddit/wiki/API) (last edited 2015-12-18; read in full)
- [reddit-archive wiki: OAuth2](https://github.com/reddit-archive/reddit/wiki/OAuth2) (last edited 2016-01-08; read in full)
- Reddit posts cited but not readable: r/modnews `1tq9vxo` "Protecting communities from scrapers and platform abuse" (2026-05-28); r/modnews `1wubgvt` "Continuing our infrastructure updates…" (2026-09-30); r/redditdev `1wubcvf` "Moving Data API apps to the Developer Platform" (2026-09-30); r/redditdev `1oug31u` "Introducing the Responsible Builder Policy…" (2025-11-11).

Secondary (pointers and observed behaviour only):

- [TechCrunch, "Reddit is killing RSS feeds, ending public API access because of AI bots"](https://techcrunch.com/2026/09/30/reddit-is-killing-rss-feeds-ending-public-api-access-because-of-ai-bots/) (2026-09-30)
- [Michael Tsai, "Reddit to Remove RSS"](https://mjtsai.com/blog/2026/10/06/reddit-to-remove-rss/) (2026-10-06)
- [PPC Land, "Reddit to halt public Data API access in March 2027 as 14,000 apps register"](https://ppc.land/reddit-to-halt-public-data-api-access-in-march-2027-as-14-000-apps-register/) (Oct–Nov 2026)
- [DEV Community, "Why Reddit blocked unauthenticated JSON in 2026"](https://dev.to/tonywangca/why-reddit-blocked-unauthenticated-json-in-2026-and-how-to-still-get-reddit-data-58b9) (2026)
- [Hushread, "Reddit RSS Not Working? Every Error and Fix"](https://hushread.com/blog/reddit-rss-not-working) (Oct 2026)
- [Feeder, "Reddit RSS feeds: what stopped and what works instead"](https://feeder.co/knowledge-base/rss-feed-creation/reddit-rss/) (updated 2026-11-21)
- [Self-Host Weekly, 2026-11-20](https://selfh.st/weekly/2026-11-20) (2026-11-20)
