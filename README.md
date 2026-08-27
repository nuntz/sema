# Sema

Sema is a cloud feed reader built for triage first: a fast, keyboard-driven grid makes the likely-interesting items physically larger, while everything unkept expires after seven days. Hearted items are copied into a permanent archive.

## Highlights

- Read RSS, Atom, JSON Feed, and YouTube channel feeds.
- Import subscriptions from OPML.
- Triage quickly with keyboard navigation and a responsive justified grid.
- Rank items using explicit feedback, reading behaviour, and Bedrock embeddings.
- Keep selected articles permanently while the unkept queue expires automatically.
- Install the SolidJS frontend as a PWA on desktop or mobile.

## How it works

```text
EventBridge -> scheduler -> feeds-q -> feed-worker -> items-q -> item-worker
                              |             |             |
                              v             v             v
                            DLQ         DynamoDB     DynamoDB + S3 + Bedrock

browser -> CloudFront -> S3 app/content
                    \-> HTTP API -> api Lambda

EventBridge (nightly) -> rescore Lambda -> updated rankings
EventBridge (weekly)  -> vector-cleanup Lambda -> expired vectors
```

The repository contains six Go Lambda functions, shared feed-processing packages, a SolidJS/TypeScript PWA, and a Pulumi Go project that provisions the AWS stack. Queue consumers are retry-safe and use dead-letter queues. DynamoDB TTL and S3 lifecycle rules enforce the rolling seven-day window; archived items and preference signals do not expire.

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
