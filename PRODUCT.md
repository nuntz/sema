# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

One person: the author, reading their own Feeds every day. Sema is a personal tool. Per-user sign-in, per-user caching and the public Apache-2.0 repository are engineering hygiene, not an audience; no decision should be bent toward invitees, self-hosters or strangers.

The job is triage: get through everything that arrived, decide quickly what deserves attention, read or watch what does, Keep the little worth having forever, and let the rest Expire without guilt.

## Product Purpose

Sema is a cloud feed reader built for triage first. It reads RSS, Atom, JSON Feed and YouTube, ranks every live Item by predicted interest, and lays them out on a justified grid where the Items most likely to matter are physically larger. Anything not Kept Expires seven days after publication; Kept Items move to a permanent Archive.

Success is a reading ritual that ends: the user triages everything that arrived in one sitting, on whichever device is at hand, and never faces a growing backlog.

## Positioning

Triage, not inbox. Three mechanisms together, none of which a conventional feed reader has:

- **Size follows interest.** Each cell's Size (small, medium, large) comes from a score built from the user's Signals (Boost, Bury, Keep), Behaviour, and text and lead-image embeddings, so the grid itself is the ranking.
- **Stories merge coverage.** When two or more Feeds cover the same thing, the Front Page shows one Story with a Lead and Headlines instead of repeats.
- **The Live Window forgets.** Unkept Items Expire after seven days and never return on Republish, so nothing accumulates and unread counts can't become debt.

## Operating Context

- Two primary rituals, equally important: keyboard-driven triage at a desktop browser, and touch triage in the installed iOS PWA. Neither is the fallback; both get full care.
- The grid shows the Front Page, Chrono order or the Archive, optionally restricted to one Scope (a Tag or Feed), within a Window (Today, Yesterday, All) that combines with showing unread Items only.
- Overlays, such as the Peek and Search, own the keyboard and the back button while on top. Video Items Play inline and can Dock in a corner while the user keeps triaging.
- Send pushes one Item on demand to a single user-chosen HTTPS Destination as a signed request; what the Destination does is not Sema's business.
- Search covers live and Kept Items by keyword, meaning and image content.

## Capabilities and Constraints

- Terminology follows `GLOSSARY.md`: UI copy, code and design docs use its terms (Keep, not save or favourite; Expire, not delete; Peek, not lightbox) and avoid its listed synonyms.
- Header, reader, grid cell and Peek chrome follow fixed, partly e2e-asserted contracts in `CODING_STANDARDS.md` (heights, insets, the 724px phone edge, media surfaces staying dark). Design work changes them deliberately or not at all.
- Motion depends on the input behind it (`web/src/input-modality.ts`): a surface opened by key does not slide in the way one opened by pointer or touch does.
- Stack is in place: SolidJS/TypeScript PWA in `web/`, Go Lambdas behind CloudFront, Pulumi infra. No local API exists; the Vite dev server proxies `/api` to a deployed stack and cannot load stored bodies or images.
- Reddit support ended in November 2026 (ADR 0009); Kept Reddit Items keep their own rendering, badge and thread link.
- Send's payload is a versioned public contract (ADR 0001); the UI never names a specific Destination.

## Brand Commitments

- Name: **Sema**. Manifest tagline: "Triage your feeds at a glance."
- Mark and icons: `web/public/sema-mark.svg`, `sema-mark-small.svg`, `favicon.svg` and the PWA icon set.
- Voice: plain and exact, using the glossary's words. Explanations say what happened and why, without apology or hype (as with the Reddit notice).

## Evidence on Hand

- `docs/screenshot.png`: the Front Page with a two-Source Story.
- The live production stack and the author's own Feeds are the only real content. There are no other users, testimonials, metrics or press; none may be invented.

## Product Principles

1. **Triage ends.** Every surface helps the user finish, not accumulate. Forgetting is a feature; counts and states must never read as debt.
2. **The grid is the ranking.** Interest is shown through Size and position, not badges or scores the user has to read.
3. **Desktop keyboard and phone touch are both first-class.** A change is finished only when it is right in both rituals.
4. **Fast and certain.** Actions respond optimistically and immediately; the user never waits on or second-guesses a Read, Keep or Send.
5. **Content leads, chrome recedes.** Images, titles and reading come first; controls stay quiet until needed.

## Accessibility & Inclusion

No named standard. Follow good practice: full keyboard operation, respect for reduced motion, sufficient contrast in both light and dark themes, and safe-area handling on iOS.
