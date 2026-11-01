# Repository Guidelines

## Project Structure & Module Organization

`cmd/<name>/` contains Go Lambda entry points and maintenance tools; reusable packages live in `internal/`, with `*_test.go` beside code. The SolidJS/TypeScript PWA lives in `web/`: `src/` holds code and unit tests, `e2e/` browser tests, and `public/` assets. `infra/` is a separate Pulumi Go module. Treat `bin/` and `web/dist/` as generated.

Ranking uses parallel text and lead-image embedding channels; image embeddings use the largest stored JPEG variant at or below 768px, and videos are always excluded.

Change an `R#` marker and its `unread_pk` in one transaction conditioned on the live row existing, and keep optimistic unread UI rather than reloading to confirm; see `docs/adr/0002-sparse-unread-index.md`.

Video playback keeps one fixed iframe host positioned over its slot, because moving the iframe reloads it; read `docs/adr/0003-video-dock-fixed-iframe-host.md` before changing the Peek, reader lead, or Dock.

Send is a public webhook contract: within `version: 1` only add optional fields, never name a specific Destination in code or docs, and never sign stored keys for a receiver; see `docs/adr/0001-send-public-webhook-contract.md`.

## Build, Test, and Development Commands

- `make test` runs root Go, Pulumi, and frontend unit tests.
- `make build` creates arm64 Lambda ZIPs and the production SPA.
- `cd web && bun run dev` starts Vite; `/api` proxies to `localhost:8787`.
- `cd web && bun run lint` checks TypeScript with Biome; `bun run format` formats it.
- `cd web && bun run test:e2e` runs Playwright after `bunx playwright install chromium`.
- `make preview` previews Pulumi changes. Reserve `make deploy` for intentional AWS deployments.
- Every green push to `master` deploys production from CI; `RUNBOOK.md` covers CI deploys, deploys by hand, and rollback. The deploy role and the Lambda boundary are fenced on purpose: never widen them or bypass the boundary to make a deploy pass; see `docs/adr/0004-ci-deploys-through-github-oidc.md`.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use tabs and lowercase package names. Use kebab-case for command directories such as `backfill-search-text`. Biome enforces two-space TypeScript indentation, double quotes, trailing commas, and recommended lint rules. Name Solid components in PascalCase (`SearchResults.tsx`) and utility modules descriptively (`read-state.ts`).

## UI Contracts

At viewports 620px and wider, keep grid and reader headers on a 56px shell with 20px edge padding, a 60px brand slot, and a 2px bottom band. The phone reader uses a 44px translucent nav bar and a toolbar sharing one material, with 0.5px hairlines and content scrolling under both. At 1200px and wider, align reader identity to the 640px article measure; collapse the shim below 1200px. Trailing icons navigate elsewhere; leading icons act on the current item. At 620px and wider, the reader read-progress gauge is 2px inside the header band, centred on the article measure, and quiet by default. The scrolled header title is the one sans-serif text in the header; all other header text stays mono. Below 860px the grid header carries the active Scope chip and a filter summary, with a 52px tag strip under it that collapses on scroll while the header never does; at 430px and below the grid header is 44px. Every header adds the top safe-area inset above these heights. In the installed iOS app, the root background matches the bar on top, because iOS tints the status bar from it. The phone reader and lightbox bars add 16px below the inset to clear the system edge blur. The phone reader toolbar sits 12px into the home-indicator inset.

## Testing Guidelines

Go tests use the standard `testing` package; frontend units use Vitest with `*.test.ts`/`*.test.tsx`, and Playwright specs use `*.e2e.ts`. Add focused regression tests beside changed code. There is no fixed coverage threshold, but CI must remain green. To exercise DynamoDB access patterns locally, run `DYNAMODB_ENDPOINT=http://localhost:8000 go test ./internal/store` with DynamoDB Local running.

## Commit & Pull Request Guidelines

Recent history favors short imperative subjects, often Conventional Commit style: `fix(web): restore settings keyboard navigation`. Prefer `type(scope): summary` when a scope is useful. Pull requests should explain behavior and affected modules, link relevant issues, list validation commands, and include screenshots for visible UI changes. Call out Pulumi/configuration changes and rollout steps. Schema changes must ship writers first; backfills under `cmd/backfill-*` should be idempotent, dry-run by default, and require `--apply` to mutate data.

## Security & Configuration

Keep credentials out of Git. Store the browser OAuth ID in `web/.env.local`, and keep Pulumi config, secrets included, in the `untz/sema/prod` ESC environment (`pulumi env set --secret`), never in stack files; see `RUNBOOK.md`.

## Agent skills

### Issue tracker

Issues live in GitHub Issues (`nuntz/sema`) via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: glossary in `CONTEXT.md`, ADRs in `docs/adr/`. See `docs/agents/domain.md`.
