# Repository Guidelines

## Project Structure & Module Organization

`cmd/<name>/` contains Go Lambda entry points and maintenance tools; reusable packages live in `internal/`, with `*_test.go` beside code. The SolidJS/TypeScript PWA lives in `web/`: `src/` holds code and unit tests, `e2e/` browser tests, and `public/` assets. `infra/` is a separate Pulumi Go module. Treat `bin/` and `web/dist/` as generated.

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

Keep grid and reader headers on a 56px shell with 20px edge padding, a 60px brand slot, and a 2px bottom band. At 1200px and wider, align reader identity to the 640px article measure; collapse the shim below 1200px. Trailing icons navigate elsewhere; leading icons act on the current item.

## Testing Guidelines

Go tests use the standard `testing` package; frontend units use Vitest with `*.test.ts`/`*.test.tsx`, and Playwright specs use `*.e2e.ts`. Add focused regression tests beside changed code. There is no fixed coverage threshold, but CI must remain green. To exercise DynamoDB access patterns locally, run `DYNAMODB_ENDPOINT=http://localhost:8000 go test ./internal/store` with DynamoDB Local running.

## Commit & Pull Request Guidelines

Recent history favors short imperative subjects, often Conventional Commit style: `fix(web): restore settings keyboard navigation`. Prefer `type(scope): summary` when a scope is useful. Pull requests should explain behavior and affected modules, link relevant issues, list validation commands, and include screenshots for visible UI changes. Call out Pulumi/configuration changes and rollout steps. Schema changes must ship writers first; backfills under `cmd/backfill-*` should be idempotent, dry-run by default, and require `--apply` to mutate data.

## Security & Configuration

Keep credentials out of Git. Store the browser OAuth ID in `web/.env.local`, and set Pulumi secrets with `pulumi config set --secret`.
