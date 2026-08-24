# Sema

Sema is a cloud feed reader built for triage first: a fast, keyboard-driven grid makes the likely-interesting items physically larger, while everything unkept expires after seven days. Hearted items are copied into a permanent archive.

This repository contains the complete MVP stack:

- six Go Lambda functions (`scheduler`, `feed-worker`, `item-worker`, `api`, `rescore`, and `vector-cleanup`);
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

The stack exports `url`, `cloudfrontUrl`, `distributionId`, `appBucket`, `contentBucket`, and `apiEndpoint`. Add the `url` HTTPS origin to the Google OAuth client's authorized JavaScript origins before signing in; sign-in fails until that origin is registered. The OAuth client ID is public browser configuration; Pulumi injects it into the Vite build automatically.

### Custom domain

Set the hostname and, when the zone is in Route 53, its hosted-zone ID:

```sh
cd infra
pulumi config set sema:domain reader.example.com
pulumi config set sema:route53ZoneId Z0123456789ABCDEF
pulumi up
```

Pulumi requests and DNS-validates an ACM certificate in `us-east-1`, attaches it as a CloudFront alias, and creates A and AAAA alias records. The default `*.cloudfront.net` hostname remains active and is exported as `cloudfrontUrl`.

When DNS is hosted elsewhere, omit `sema:route53ZoneId`. Run a targeted update for `domain-certificate` first, publish the exported `certificateValidationName` / `certificateValidationValue` CNAME at the DNS provider, then run the full update. Point the app hostname at the exported `dnsCnameTarget`. Apex domains need an ALIAS/ANAME-style record from the DNS provider; ordinary subdomains can use CNAME.

After the domain resolves, add `https://reader.example.com` (using your configured name) to the Google OAuth web client’s **Authorized JavaScript origins**. If the GIS client is separately origin-restricted, update that configuration too. Keep the CloudFront origin registered until every installed PWA has moved to the custom origin.

Pulumi generates an RSA key in encrypted stack state, registers its public half with CloudFront, and supplies the secret half only to the API Lambda. Authenticated API responses refresh one-hour signed cookies scoped to `/bodies/<sub>/`, `/media/<sub>/`, and `/archive/<sub>/`, which are also the object keys in the content bucket; favicons are shared public assets. S3 itself remains private behind origin access control.

`pulumi up` runs the pinned Bun install and builds the Lambda archives and SPA before registering their assets, so a clean checkout does not need a separate build command. `make deploy` is an equivalent shortcut from the repository root.

## Schema changes

- Any new index or key attribute ships in two deploys: deploy writers, create the index, and run the backfill first; switch readers only after the backfill is verified.
- Every backfill is a one-off idempotent command under `cmd/` (for example, `cmd/backfill-<name>`) that dry-runs by default, prints affected and total row counts, and applies changes only with `--apply`.
- Each such change includes a test, written alongside the change, proving that the reader path still handles rows missing the new attribute.

### Item identity marker rollout

Live item sort keys include their publication timestamp, so Sema also keeps a TTL-backed `D#<item_id>` marker to enforce stable identity when a feed republishes an entry with a changed date. Before deploying the marker-aware item worker over a stack with existing items, create markers and reconcile duplicate live rows:

```sh
make backfill-item-identities STACK=prod
make backfill-item-identities STACK=prod BACKFILL_ARGS=--apply
make deploy
make backfill-item-identities STACK=prod BACKFILL_ARGS=--apply
```

The command is a read-only dry run unless `--apply` is supplied. It keeps the newest live row for each `item_id`, preserves any archive pointer on that row, creates or refreshes its identity marker, and deletes older duplicate live rows atomically. The second applied run closes the small window in which the old worker may have written an unmarked item during deployment. Read, signal, behaviour, and archive rows are already keyed by `item_id` and are not removed.

### Search rollout and vector migrations

Search attributes follow the writers-first convention. Deploy the item worker
and heart writer, reindex live rows, backfill archive rows, and only then expose
the search endpoint:

```sh
make replay STACK=prod FLAGS="--reindex"
make backfill-search-text STACK=prod
make backfill-search-text STACK=prod BACKFILL_ARGS=--apply
make backfill-vectors STACK=prod
make backfill-vectors STACK=prod BACKFILL_ARGS=--apply
```

Both backfills are dry-run by default and print total and affected row counts.
The vector backfill indexes every current live embedding and every archive
embedding; it does not invoke Bedrock or change ranking rows.

The real-service vector lifecycle test is opt-in and refuses non-dev buckets:

```sh
S3_VECTORS_INTEGRATION=1 \
  VECTOR_BUCKET="$(cd infra && pulumi stack output vectorBucket --stack dev)" \
  VECTOR_INDEX="$(cd infra && pulumi stack output vectorIndex --stack dev)" \
  go test ./internal/vectorstore -run TestS3VectorsDevIntegration
```

The vector index name and dimensions live beside `modelVersion` as
`sema:vectorIndex` and `sema:vectorDimensions`. An embedding-model change that
alters dimensions requires a deliberate index migration: create a new index,
dual-write it, replay the current window and archive, switch reads, verify, and
only then delete the old index. Never resize or repurpose an existing index in
place.

### YouTube connector rollout

YouTube channel uploads use the public 15-entry Atom feed, so uploads older
than the channel feed at add time cannot be recovered. Deploy connector-aware
writers and the worker registry before switching discovery, then migrate the
Drop 3 feed rows in place:

```sh
cd infra
pulumi config set sema:youtubeDiscoveryEnabled false --stack prod
cd ..
make deploy
make backfill-youtube-connector STACK=prod
make backfill-youtube-connector STACK=prod BACKFILL_ARGS=--apply
cd infra
pulumi config set sema:youtubeDiscoveryEnabled true --stack prod
cd ..
make deploy
```

The backfill is a dry run unless `--apply` is supplied. It recognizes stored
YouTube uploads URLs, preserves each URL and `feed_id`, sets
`connector=youtube`, and caches the channel's 96px avatar. Because item IDs
still derive from the same feed ID and entry GUID, live history, archive
pointers, signals, and read state continue without duplication.
`youtubeDiscoveryEnabled` defaults to true for new stacks; the temporary false
value is only the rollout gate that keeps new YouTube adds behind the backfill.

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
pulumi config set sema:summarizeModel amazon.nova-micro-v1:0
pulumi up
cd ..
make replay STACK=dev MODEL_VERSION=amazon.titan-embed-text-v2:0
make replay STACK=dev FLAGS="--force-extract --force-summary"
make rescore STACK=dev
```

Replay re-embeds retained `S#` and `B#` rows from their stored titles before
queueing every live item with `reprocess: true`. The MODEL row records the
replay start and target version; scheduled rescoring pauses for one hour while
that marker is active. An on-demand rescore is allowed to complete the
migration and clears the marker. Rows tagged with a different model version
are never combined in a centroid. Stored titles are the replay fallback;
archived bodies are deliberately not replayed: a kept body is frozen at heart
time. Replay overwrites only live item rows and leaves read, signal, behaviour,
heart, and `hearted_ts` state untouched.

## Runtime flow

```text
EventBridge -> scheduler -> feeds-q -> feed-worker -> items-q -> item-worker
                              |             |             |
                              v             v             v
                            DLQ         DynamoDB     DynamoDB + S3 + Bedrock

browser -> CloudFront -> S3 app/content
                    \-> HTTP API (Google JWT) -> api Lambda

EventBridge (09:00 UTC) -> rescore Lambda -> MODEL + live item scores
EventBridge (weekly) -> vector-cleanup Lambda -> expired live vectors
```

Feed and item queue consumers use partial-batch failure reporting. Fetches are bounded by time, redirect count, and body size. Item storage is deterministic and conditionally written, so SQS retries can safely repeat extraction and object uploads. DynamoDB TTL and prefix-scoped S3 lifecycle policies enforce the seven-day rolling window; archive and signal records intentionally have no TTL.

To move all messages from both dead-letter queues back to their source queues without opening the AWS console:

```sh
make redrive STACK=dev
```

The command starts one SQS `StartMessageMoveTask` for `feeds-dlq` and one for `items-dlq`. Use the target stack’s AWS credentials and region.

## Tests

The default suite covers feed normalization, OPML, sanitization, score math, timestamp ordering, signed content cookies, row justification, per-item read batching and undo, new-item insertion, and keyboard/layout behavior.

The DynamoDB access-pattern test runs when `DYNAMODB_ENDPOINT` is set. CI starts DynamoDB Local and exercises conditional dedupe, both item orders, expiry filtering, read rows, and persistent signal rows.

```sh
DYNAMODB_ENDPOINT=http://localhost:8000 go test ./internal/store
```
