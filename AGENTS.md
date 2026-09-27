# Repository Guidelines

## Project Structure & Module Organization

`cmd/<name>/` contains Go Lambda entry points and maintenance tools; reusable packages live in `internal/`, with `*_test.go` beside code. The SolidJS/TypeScript PWA lives in `web/`: `src/` holds code and unit tests, `e2e/` browser tests, and `public/` assets. `infra/` is a separate Pulumi Go module. Treat `bin/` and `web/dist/` as generated.

Ranking uses parallel text and lead-image embedding channels; image embeddings use the largest stored JPEG variant at or below 768px, and videos are always excluded.

Send posts one Item to the user's Destination under a public webhook contract; within `version: 1` only add optional fields, and never name a specific Destination in code or docs. Payload fields follow `CONTEXT.md` (`kept`, `feed`, feed `tags`), and `images` lists the stored origin lead URL first, then a one-hour CloudFront-signed URL to a per-Send copy of Sema's largest stored image under `send/`, so receivers never depend on publisher pages that block them; stored keys embed the Google subject, so never sign them for a receiver. The first attempt runs inline in the API request so the user sees the real status; retryable failures go to the deliveries queue, re-signed with the current secret on each attempt. The Destination and its secret live in a separate `DESTINATION` row, never the profile, and the secret is write-only over the API.

## Build, Test, and Development Commands

- `make test` runs root Go, Pulumi, and frontend unit tests.
- `make build` creates arm64 Lambda ZIPs and the production SPA.
- `cd web && bun run dev` starts Vite; `/api` proxies to `localhost:8787`.
- `cd web && bun run lint` checks TypeScript with Biome; `bun run format` formats it.
- `cd web && bun run test:e2e` runs Playwright after `bunx playwright install chromium`.
- `make preview` previews Pulumi changes. Reserve `make deploy` for intentional AWS deployments.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use tabs and lowercase package names. Use kebab-case for command directories such as `backfill-search-text`. Biome enforces two-space TypeScript indentation, double quotes, trailing commas, and recommended lint rules. Name Solid components in PascalCase (`SearchResults.tsx`) and utility modules descriptively (`read-state.ts`).

## UI Contracts

At viewports 620px and wider, keep grid and reader headers on a 56px shell with 20px edge padding, a 60px brand slot, and a 2px bottom band. The phone reader uses a 44px translucent nav bar and a toolbar sharing one material, with 0.5px hairlines and content scrolling under both. At 1200px and wider, align reader identity to the 640px article measure; collapse the shim below 1200px. Trailing icons navigate elsewhere; leading icons act on the current item. At 620px and wider, the reader read-progress gauge is 2px inside the header band, centred on the article measure, and quiet by default. The scrolled header title is the one sans-serif text in the header; all other header text stays mono.

## Testing Guidelines

Go tests use the standard `testing` package; frontend units use Vitest with `*.test.ts`/`*.test.tsx`, and Playwright specs use `*.e2e.ts`. Add focused regression tests beside changed code. There is no fixed coverage threshold, but CI must remain green. To exercise DynamoDB access patterns locally, run `DYNAMODB_ENDPOINT=http://localhost:8000 go test ./internal/store` with DynamoDB Local running.

## Commit & Pull Request Guidelines

Recent history favors short imperative subjects, often Conventional Commit style: `fix(web): restore settings keyboard navigation`. Prefer `type(scope): summary` when a scope is useful. Pull requests should explain behavior and affected modules, link relevant issues, list validation commands, and include screenshots for visible UI changes. Call out Pulumi/configuration changes and rollout steps. Schema changes must ship writers first; backfills under `cmd/backfill-*` should be idempotent, dry-run by default, and require `--apply` to mutate data.

## Security & Configuration

Keep credentials out of Git. Store the browser OAuth ID in `web/.env.local`, and set Pulumi secrets with `pulumi config set --secret`.
