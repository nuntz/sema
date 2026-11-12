# Repository Guidelines

## Project Structure & Module Organization

`cmd/<name>/` contains Go Lambda entry points and maintenance tools; reusable packages live in `internal/`, with `*_test.go` beside code. The SolidJS/TypeScript PWA lives in `web/`: `src/` holds code and unit tests, `e2e/` browser tests, and `public/` assets. `infra/` is a separate Pulumi Go module. Treat `bin/` and `web/dist/` as generated.

Ranking uses parallel text and lead-image embedding channels; image embeddings use the largest stored JPEG variant at or below 768px, and videos are always excluded.

Change an `R#` marker and its `unread_pk` in one transaction conditioned on the live row existing, then bump `READ_VERSION`, and keep optimistic unread UI rather than reloading to confirm; see `docs/adr/0002-sparse-unread-index.md` and `docs/adr/0005-read-state-is-read-your-writes.md`.

Per-user caching and its version rows live in the Request snapshot (`cmd/api/internal/requestsnapshot`); see ADRs 0005–0007.

Video playback keeps one fixed iframe host positioned over its slot, because moving the iframe reloads it; read `docs/adr/0003-video-dock-fixed-iframe-host.md` before changing the Peek, reader lead, or Dock.

Send is a public webhook contract: within `version: 1` only add optional fields, never name a specific Destination in code or docs, and never sign stored keys for a receiver; see `docs/adr/0001-send-public-webhook-contract.md`.

## Build, Test, and Development Commands

- `make check` is CI's test job (lint, format, vet, unit tests, web build, reproducible Lambda ZIPs); run it before every commit, plus `make e2e` when `web/` changed. `make test` runs only the unit tests.
- `make build` creates arm64 Lambda ZIPs and the production SPA.
- `cd web && bun run dev` starts Vite and proxies `/api` to `SEMA_API_ORIGIN`, such as a deployed stack's `cloudfrontUrl`. No local API server exists (`cmd/api` runs only as a Lambda), and only `/api` is proxied, so stored bodies and images don't load.
- `cd web && bun run lint` checks TypeScript with Biome; `bun run format` formats it.
- `make e2e` is CI's e2e job (Playwright), after `cd web && bunx playwright install chromium`.
- `make preview` previews Pulumi changes. Reserve `make deploy` for intentional AWS deployments.
- Every green push to `master` deploys production from CI; `RUNBOOK.md` covers CI deploys, confirming one, deploys by hand, and rollback. The deploy role and the Lambda boundary are fenced on purpose: never widen them or bypass the boundary to make a deploy pass; see `docs/adr/0004-ci-deploys-through-github-oidc.md`.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use tabs and lowercase package names. Use kebab-case for command directories such as `backfill-search-text`. Biome enforces two-space TypeScript indentation, double quotes, trailing commas, and recommended lint rules. Name Solid components in PascalCase (`SearchResults.tsx`) and utility modules descriptively (`read-state.ts`).

## UI Contracts

Header, reader, grid, and lightbox chrome follow fixed contracts (heights, insets, type, icon roles) in `CODING_STANDARDS.md`; read them before changing that chrome. `web/e2e/header.e2e.ts` asserts the measurable ones.

## Testing Guidelines

Go tests use the standard `testing` package; frontend units use Vitest with `*.test.ts`/`*.test.tsx`, and Playwright specs use `*.e2e.ts`. Add focused regression tests beside changed code. There is no fixed coverage threshold, but CI must remain green. Tests on DynamoDB Local skip without `DYNAMODB_ENDPOINT`; `make check` starts it, and `scripts/with-dynamodb-local.sh go test ./cmd/api/...` runs one package against it.

## Commit & Pull Request Guidelines

Recent history favors short imperative subjects, often Conventional Commit style: `fix(web): restore settings keyboard navigation`. Prefer `type(scope): summary` when a scope is useful. Pull requests should explain behavior and affected modules, link relevant issues, list validation commands, and include screenshots for visible UI changes. Call out Pulumi/configuration changes and rollout steps. Schema changes must ship writers first; backfills under `cmd/backfill-*` should be idempotent, dry-run by default, and require `--apply` to mutate data.

## Security & Configuration

Keep credentials out of Git. Store the browser OAuth ID in `web/.env.local`, and keep Pulumi config, secrets included, in the `untz/sema/prod` ESC environment (`pulumi env set --secret`), never in stack files; see `RUNBOOK.md`.

## Agent skills

### Issue tracker

Issues live in GitHub Issues (`nuntz/sema`) via the `gh` CLI. See `~/repos/home/docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `~/repos/home/docs/agents/triage-labels.md`.

### Domain docs

Single-context: glossary in `CONTEXT.md`, ADRs in `docs/adr/`. See `docs/agents/domain.md`.
