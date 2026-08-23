# Sema

Sema is a cloud feed reader built for triage first: a fast, keyboard-driven grid makes the likely-interesting items physically larger, while everything unkept expires after seven days. Hearted items are copied into a permanent archive.

This repository contains the complete MVP stack:

- five Go Lambda functions (`scheduler`, `feed-worker`, `item-worker`, `api`, and `rescore`);
- RSS, Atom, JSON Feed, OPML, extraction, sanitization, media, embeddings, and scoring packages;
- a SolidJS/TypeScript PWA with a virtualized justified grid and overlay reader;
- one Pulumi Go project for DynamoDB, S3, CloudFront, SQS, API Gateway, IAM, EventBridge, and alarms.

## Requirements

- Go 1.26+
- Bun 1.3+
- Pulumi 3+
- `zip` on `PATH` (the Lambda archive step shells out to it)
- AWS credentials for the target account (`aws sts get-caller-identity` must succeed)
- Bedrock model access to `amazon.titan-embed-text-v2:0` in `us-east-1`
- a Google OAuth web client ID

## Verify and build

```sh
make test
make build
```

`make build` creates five arm64 Lambda archives in `bin/` and the production SPA in `web/dist/`. The item worker uses the spec-approved JPEG q85 fallback because its image path is fully static and CGO-free; the object key and content type accurately use `.jpg`/`image/jpeg`.

Frontend-only commands:

```sh
cd web
bun install --frozen-lockfile
bun run dev
bun run lint
bun run test
```

For local frontend work, put the Google client ID in `web/.env.local` as `VITE_GOOGLE_CLIENT_ID`. Vite proxies `/api` to `http://localhost:8787` by default; set `SEMA_API_ORIGIN` to proxy a deployed stack.

## Deploy

```sh
cd infra
pulumi stack init dev                                    # or: pulumi stack select dev
pulumi config set --secret sema:googleClientId YOUR_CLIENT_ID
pulumi up
```

`Pulumi.dev.yaml` and `Pulumi.prod.yaml` are checked in with `aws:region: us-east-1`, so the client ID is the only configuration a new stack needs. `Pulumi.yaml` declares `sema:googleClientId` as a secret, so set it with `--secret`.

The stack exports `url`, `distributionId`, `appBucket`, `contentBucket`, and `apiEndpoint`. Add the `url` HTTPS origin to the Google OAuth client's authorized JavaScript origins before signing in; sign-in fails until that origin is registered. The OAuth client ID is public browser configuration; Pulumi injects it into the Vite build automatically.

Pulumi generates an RSA key in encrypted stack state, registers its public half with CloudFront, and supplies the secret half only to the API Lambda. Authenticated API responses refresh one-hour signed cookies scoped to `/bodies/<sub>/`, `/media/<sub>/`, and `/archive/<sub>/`, which are also the object keys in the content bucket; favicons are shared public assets. S3 itself remains private behind origin access control.

`pulumi up` runs the pinned Bun install and builds the Lambda archives and SPA before registering their assets, so a clean checkout does not need a separate build command. `make deploy` is an equivalent shortcut from the repository root.

## Schema changes

- Any new index or key attribute ships in two deploys: deploy writers, create the index, and run the backfill first; switch readers only after the backfill is verified.
- Every backfill is a one-off idempotent command under `cmd/` (for example, `cmd/backfill-<name>`) that dry-runs by default, prints affected and total row counts, and applies changes only with `--apply`.
- Each such change includes a test, written alongside the change, proving that the reader path still handles rows missing the new attribute.

## Ranking maintenance

The nightly `rescore` Lambda rebuilds each MODEL row from explicit and
behaviour signals, then updates scores, size buckets, and explanations across
the live seven-day window. It can be run on demand:

```sh
make rescore STACK=dev
```

For an embedding-model upgrade, set a new version and replay the window before
the on-demand rescore:

```sh
cd infra
pulumi config set sema:modelVersion amazon.titan-embed-text-v2:0
pulumi up
cd ..
make replay STACK=dev MODEL_VERSION=amazon.titan-embed-text-v2:0
make rescore STACK=dev
```

Replay re-embeds retained `S#` and `B#` rows from their stored titles before
queueing every live item with `reprocess: true`. The MODEL row records the
replay start and target version; scheduled rescoring pauses for one hour while
that marker is active. An on-demand rescore is allowed to complete the
migration and clears the marker. Rows tagged with a different model version
are never combined in a centroid. Stored titles are the replay fallback;
archived bodies can be used by a future higher-fidelity migration command.

## Runtime flow

```text
EventBridge -> scheduler -> feeds-q -> feed-worker -> items-q -> item-worker
                              |             |             |
                              v             v             v
                            DLQ         DynamoDB     DynamoDB + S3 + Bedrock

browser -> CloudFront -> S3 app/content
                    \-> HTTP API (Google JWT) -> api Lambda

EventBridge (09:00 UTC) -> rescore Lambda -> MODEL + live item scores
```

Feed and item queue consumers use partial-batch failure reporting. Fetches are bounded by time, redirect count, and body size. Item storage is deterministic and conditionally written, so SQS retries can safely repeat extraction and object uploads. DynamoDB TTL and prefix-scoped S3 lifecycle policies enforce the seven-day rolling window; archive and signal records intentionally have no TTL.

## Tests

The default suite covers feed normalization, OPML, sanitization, score math, timestamp ordering, signed content cookies, row justification, per-item read batching and undo, new-item insertion, and keyboard/layout behavior.

The DynamoDB access-pattern test runs when `DYNAMODB_ENDPOINT` is set. CI starts DynamoDB Local and exercises conditional dedupe, both item orders, expiry filtering, read rows, and persistent signal rows.

```sh
DYNAMODB_ENDPOINT=http://localhost:8000 go test ./internal/store
```
