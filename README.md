# Sema

Sema is a cloud feed reader built for triage first: a fast, keyboard-driven grid makes the likely-interesting items physically larger, while everything unkept expires after seven days. Hearted items are copied into a permanent archive.

## Highlights

- Read RSS, Atom, JSON Feed, YouTube channel feeds, and public subreddit feeds.
- Import subscriptions from OPML.
- Triage quickly with keyboard navigation and a responsive justified grid.
- Rank items using explicit feedback, reading behaviour, and Bedrock embeddings.
- Keep selected articles permanently while the unkept queue expires automatically.
- Install the SolidJS frontend as a PWA on desktop or mobile.

## How it works

```text
                              INGESTION PIPELINE
                              ==================

 EventBridge ---------+
 (rate schedule)      |
                      v
               +-------------+    claims due feeds     +----------------+
               |  scheduler  |<----------------------->|    DynamoDB    |
               |   Lambda    |   (5-min claim lease)   | feeds / items  |
               +------+------+                         | prefs / users  |
                      | enqueue feed jobs              +----------------+
                      v                                    ^         ^
               [ feeds-q SQS ]--> DLQ                      |         |
                      |                                    |         |
                      v                                    |         |
               +-------------+   fetch + parse feeds       |         |
               | feed-worker |-- RSS / Atom / JSON Feed ---+         |
               |   Lambda    |   YouTube / Reddit connectors         |
               +------+------+   (dedupe, write new items)           |
                      | enqueue per-item jobs                        |
                      v                                              |
               [ items-q SQS ]--> DLQ                                |
                      |                                              |
                      v                                              |
               +-------------+   extract article text ---------------+
               | item-worker |   fetch media/thumbnails ---> S3 (content)
               |   Lambda    |   embed text -----------> Bedrock Titan
               +-------------+                                |
                                            store vectors     v
                                            & score item   [ S3 Vectors ]


                              SERVING & TRIAGE
                              ================

  browser (SolidJS PWA)
    | Google sign-in (once) -> API sets 30-day HttpOnly session cookie
    v
 +------------+   static app & media    +--------------------------+
 | CloudFront |------------------------>| S3: app bundle + content |
 |            |   (signed cookies)      +--------------------------+
 +-----+------+
       | /api
       v
 +------------+   items, hearts, feedback, OPML import, search
 | HTTP API   |----> api Lambda ----> DynamoDB / SQS / Bedrock (query
 +------------+                       embeddings) / S3 Vectors (similarity)

  User signals fed back in:  hearts, skips, reading behaviour ---> prefs


                              RANKING & EXPIRY
                              ================

 EventBridge (nightly) --> rescore Lambda --------> re-ranks items per user
                             (blends explicit feedback + behaviour
                              + embedding similarity to liked items)

 EventBridge (weekly) ---> vector-cleanup Lambda -> deletes expired vectors

 DynamoDB TTL + S3 lifecycle: unkept items expire after 7 days
 Hearted items -> copied to permanent archive (never expires)
```

The repository contains six Go Lambda functions, shared feed-processing packages, a SolidJS/TypeScript PWA, and a Pulumi Go project that provisions the AWS stack. Queue consumers are retry-safe and use dead-letter queues. DynamoDB TTL and S3 lifecycle rules enforce the rolling seven-day window; archived items and preference signals do not expire.

## Connectors

Feed transports implement the shared `internal/connector.Connector` interface and are registered by source type in the feed worker. RSS, Atom, and JSON Feed share the generic parser; YouTube and Reddit add source-specific discovery and item metadata while retaining the same scheduling, deduplication, ranking, retry, retention, and archive paths.

The Reddit connector uses only Reddit's public Atom feeds—there is no Reddit API, OAuth, login, credential, or third-party service. Discovery accepts `r/name`, full subreddit URLs, and supported subreddit feed URLs. It canonicalizes the selected collection into the stored feed URL:

- Hot: `/r/<subreddit>/.rss`, fetched every three hours.
- Top · day: `/r/<subreddit>/top.rss?t=day`, fetched daily.
- New: `/r/<subreddit>/new.rss`, fetched hourly.

Scheduled and discovery requests send the fixed `User-Agent: linux:sema:rss`. Feed jobs retain their stable per-feed schedule offsets and generic exponential error backoff; Reddit failures remain isolated to the affected feed.

Reddit items preserve the thread permalink separately from an external article or media destination. Link posts use the normal article extraction pipeline and open in Reader, text posts render sanitized selftext already present in Atom, and image posts render in Reader with a cached-thumbnail fallback. Reddit-hosted video remains external because `v.redd.it` media is delivered as split DASH streams. The message-square action on every Reddit cell opens its comment thread externally. Titles and cleaned excerpts enter the existing embedding and ranking pipeline unchanged.

## Prerequisites

- Go 1.26+
- Bun 1.3+
- Pulumi 3+
- `zip` on `PATH`
- AWS credentials for the target account
- Bedrock model access to `amazon.titan-embed-text-v2:0` in `us-east-1`
- A Google OAuth web client ID

Confirm AWS access with `aws sts get-caller-identity` before deploying.

## Local development

Run the complete test suite and build all Lambda archives and frontend assets:

```sh
make test
make build
```

To work on the frontend:

```sh
cd web
bun install --frozen-lockfile
bun run dev
```

Set `VITE_GOOGLE_CLIENT_ID` in `web/.env.local`. Vite proxies `/api` to `http://localhost:8787`; set `SEMA_API_ORIGIN` to use a deployed API instead. See [AGENTS.md](AGENTS.md) for linting, formatting, test, and contribution guidance.

## Deploy

```sh
cd infra
pulumi stack init dev # or: pulumi stack select dev
pulumi config set --secret sema:googleClientId YOUR_CLIENT_ID
pulumi up
```

The checked-in stack files use `us-east-1`; a new stack only needs the Google client ID. `pulumi up` installs pinned frontend dependencies, builds the Lambdas and SPA, and provisions their AWS resources. From the repository root, `make deploy` performs the same update and invalidates CloudFront.

After deployment, run `pulumi stack output url` and add that HTTPS origin to the Google OAuth client’s **Authorized JavaScript origins**. Sign-in will not work until it is registered. The stack also exports the CloudFront URL, API endpoint, distribution ID, and storage bucket names.

### Authentication and private content

Google Identity Services is used only for initial sign-in. The API exchanges the Google ID token for a 30-day sliding session in a Secure, HttpOnly cookie; the browser does not store or resend the Google token. Pulumi keeps the content-signing private key in encrypted stack state, S3 remains private behind CloudFront, and authenticated responses refresh short-lived signed content cookies.

### Custom domain

Set the hostname and, when the zone is in Route 53, its hosted-zone ID:

```sh
cd infra
pulumi config set sema:domain reader.example.com
pulumi config set sema:route53ZoneId Z0123456789ABCDEF
pulumi up
```

Pulumi requests and validates the certificate, configures the CloudFront alias, and creates A and AAAA records. The default `*.cloudfront.net` URL remains available.

For external DNS, omit `sema:route53ZoneId`. First deploy the `domain-certificate` target, publish the exported validation CNAME, and then run the full update. Point subdomains to `dnsCnameTarget` with CNAME; apex domains require the provider’s ALIAS/ANAME equivalent.

After DNS resolves, add the custom HTTPS origin to the Google OAuth client. Keep the CloudFront origin registered until installed PWAs have moved to the custom domain.

## Updating an existing stack

New stacks do not need these migrations. Backfill commands start in read-only mode and report affected rows; inspect that output before running them again with `BACKFILL_ARGS=--apply`.

### Item identity marker rollout

Before deploying the identity-aware item worker over older data, create identity markers and reconcile duplicate live rows:

```sh
make backfill-item-identities STACK=prod
make backfill-item-identities STACK=prod BACKFILL_ARGS=--apply
make deploy
make backfill-item-identities STACK=prod BACKFILL_ARGS=--apply
```

The backfill keeps the newest row for each item, preserves archive pointers, and creates its identity marker. The final pass catches items written during deployment.

### Split item-vector rollout and recovery

Deploy the split-vector writer and targeted rescore updates before migrating older live items. Then inspect and apply the DynamoDB item-vector backfill before allowing a scheduled or on-demand rescore:

```sh
make deploy STACK=prod
make backfill-item-vectors STACK=prod
make backfill-item-vectors STACK=prod BACKFILL_ARGS=--apply
```

The command is idempotent and dry-run by default. It creates only missing or expired `V#` rows, preferring a surviving inline embedding and otherwise restoring the original embedding from S3 Vectors. Investigate any non-zero `unavailable` count and replay those items before rescoring. The separate `backfill-vectors` command below copies DynamoDB embeddings in the opposite direction, into the S3 vector index.

### Search rollout and vector migrations

Deploy search writers first, then reindex live items and backfill search text and vectors:

```sh
make replay STACK=prod FLAGS="--reindex"
make backfill-search-text STACK=prod
make backfill-search-text STACK=prod BACKFILL_ARGS=--apply
make backfill-vectors STACK=prod
make backfill-vectors STACK=prod BACKFILL_ARGS=--apply
```

The vector backfill reuses stored embeddings and does not invoke Bedrock or change rankings. If a new embedding model changes vector dimensions, create a new index, dual-write, replay and verify the data, switch reads, and only then remove the old index.

### YouTube connector rollout

For deployments that predate the YouTube connector, gate discovery while migrating stored channel feeds:

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

The migration preserves feed and item identities, so history, archive pointers, signals, and read state remain connected. YouTube exposes only the latest 15 uploads through its public feed; older uploads cannot be recovered during import.

### Reddit connector rollout

Deploy the connector writers before migrating subreddit feeds that were previously stored as generic RSS sources:

```sh
make deploy
make backfill-reddit-connector STACK=prod
make backfill-reddit-connector STACK=prod BACKFILL_ARGS=--apply
```

The idempotent backfill recognizes only supported subreddit feed URLs. It keeps each existing feed ID, tags, mute state, items, archive pointers, ranking history, and read state while changing the connector to `reddit`, canonicalizing the selected Hot/Top-Day/New URL, setting its cadence, and clearing URL-specific HTTP validators when required. Unsupported Reddit listing types are left unchanged.

## Operations

### Refresh rankings

The nightly `rescore` Lambda updates rankings across the live seven-day window. Run it on demand with:

```sh
make rescore STACK=dev
```

For an embedding or summary model upgrade, deploy the new configuration, replay the window, and then rescore:

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

Replay rebuilds embeddings from stored titles and reprocesses live items. It does not change read state, feedback, hearts, or archived article bodies.

### Redrive failed queue messages

Move messages from both dead-letter queues back to their source queues with:

```sh
make redrive STACK=dev
```

Use credentials for the target stack and review the queues before redriving production messages.

## License

Sema is licensed under the [Apache License 2.0](LICENSE).
