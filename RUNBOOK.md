# Sema Operations Runbook

## CI deploys

Every green push to `master` deploys `untz/sema/prod`: the `deploy` job in
`.github/workflows/ci.yml` runs after the `test` job passes. It builds the arm64 Lambda ZIPs, reads
`aws:region` and `sema:googleClientId` from the stack config, builds the SPA with that client ID
(the job fails if it is empty, rather than shipping a bundle nobody can sign in to), runs
`pulumi up --yes`, invalidates CloudFront, and runs the live check, `scripts/check-live.sh`: the
site root must answer 200 with the SPA's HTML, and `/api/items` 401 without a session. Pull
requests run the tests and the workflow and shell-script lint (`scripts/lint-ci-files.sh`, also
runnable locally with podman or docker) but never deploy.

A push is not a deploy until its run finishes: after pushing `master`, run
`bash scripts/watch-deploy.sh`, which fails unless test, e2e and deploy all succeed for `HEAD`.
GitHub can leave a push's run queued with no jobs for hours, and it can't be cancelled or re-run;
the script fails on a run queued over ten minutes, and `gh workflow run ci.yml --ref master` starts
a fresh one.

A stack of PRs, each based on the one below, lands one PR at a time from the bottom:
`bash scripts/land-stacked-pr.sh <pr> --apply` merges it, retargets and rebases the PRs above it,
then deletes its branch (without `--apply` it prints the plan). Each merge deploys.

The job runs in the `production` GitHub environment, which GitHub limits to `master`, with
`id-token: write`, in one `deploy` concurrency group that never cancels a run in progress. It
reaches AWS through GitHub OIDC as `sema-github-deploy`, so no AWS key is stored in GitHub; the
role and its fence are the [bootstrap stack](#bootstrap-stack)'s, and
`docs/adr/0004-ci-deploys-through-github-oidc.md` records the trust model. It reaches Pulumi Cloud
through GitHub OIDC too, so no Pulumi token is stored in GitHub either: see
[Pulumi Cloud sign-in](#pulumi-cloud-sign-in).

The job reads no secret, only four repository variables: `PULUMI_STACK` (`untz/sema/prod`),
`PULUMI_ESC_ENVIRONMENT` (`sema/prod`), `PULUMI_CI_USER` (the Pulumi user CI signs in as) and
`AWS_DEPLOY_ROLE_ARN` (the bootstrap stack's `deployRoleArn` output). They are repository
variables, not `production` environment ones, because the job decides whether to run from
`PULUMI_STACK` before it enters the environment: an environment variable would always skip it.
Until `PULUMI_STACK` is set, and on forks, the job is skipped without failing. The last stage of
`bash scripts/setup-deploy.sh` creates the environment, walks the Pulumi Cloud trust policies and
sets the variables.

When a deploy fails with `AccessDenied`, the message names the IAM action the deploy role lacks:
grant it in `infra/deployaccess`, apply the bootstrap stack by hand, then re-run the workflow.

### Pulumi Cloud sign-in

Before its first Pulumi command, the deploy job runs `pulumi/auth-actions`, pinned to a reviewed
commit. It exchanges the job's GitHub OIDC token for a one-hour personal access token of
`PULUMI_CI_USER` and exports it, masked, as `PULUMI_ACCESS_TOKEN`. The next step checks that
`pulumi whoami` is that user. Neither the GitHub token nor the Pulumi token is printed. A deploy
takes minutes, so the token outlives the job only by the rest of its hour.

The Pulumi organization (`untz`) trusts the issuer `https://token.actions.githubusercontent.com`
once, for every repository that deploys this way, with a max expiration of one hour. Sema has two
Allow policies of its own on it, one per deploy event: `push`, and `workflow_dispatch` for
[rollbacks](#rollback). Each issues a Personal token of `PULUMI_CI_USER` and matches these
claims exactly:

| Claim | Value |
|---|---|
| `aud` | `urn:pulumi:org:untz` |
| `sub` | `repo:nuntz@261462/sema@1342395675:environment:production` |
| `environment` | `production` |
| `event_name` | `push`, or `workflow_dispatch` |
| `ref`, `ref_type` | `refs/heads/master`, `branch` |
| `repository`, `repository_id`, `repository_owner_id` | `nuntz/sema`, `1342395675`, `261462` |
| `workflow_ref`, `job_workflow_ref` | `nuntz/sema/.github/workflows/ci.yml@refs/heads/master` |
| `runner_environment` | `github-hosted` |

The `sub` is the immutable subject the AWS role trusts too (`sema:githubSubjectPrefix` plus the
environment). There is no wildcard and no `repo:nuntz/sema:…` subject. A token therefore needs this
repository by ID, the `production` environment, `master`, this workflow file, a GitHub-hosted
runner and a push or dispatch: pull requests, other branches and workflows, forks and a recreated
repository of the same name can't get one. The `environment` and `ref` rules overlap on purpose:
GitHub's branch policy on `production` and Pulumi's `ref` rule each still hold if the other is
loosened.

Every rule of a policy must match, and policies only add access, so the policies other
repositories keep on the shared issuer, each naming its own repository, can't issue Sema's token,
and Sema's can't issue theirs. Leave them alone when editing Sema's. In a rule value, `*` and `?`
are wildcards and `.` matches any one character, so `ci.yml` alone would also match `ciXyml`: the
numeric `repository_id` and `repository_owner_id` rules are what pin the repository. They also let
tooling that rewrites the issuer's policy list tell Sema's policies from its own and keep them.
The wizard's last stage walks Sema's policies and reads them back to check them; to check by
hand, open the organization's Settings → Access Management → OIDC Issuers → `github-actions`.

Pulumi's free tier issues only personal OIDC tokens, so CI acts as that user, with all of its
access: every organization and stack it belongs to, decrypting stack state, and ESC. The
repository's name doesn't narrow that. What OIDC removes is a long-lived token stored in GitHub: a
compromised action in the deploy job only gets a token that lasts the hour.

To validate a change to the trust, push to `master` (or run the workflow on it) and read the deploy
log: the auth step says `OIDC token exchange performed successfully.`, the identity check prints
the expected user, and the stack and its ESC environment resolve. A failed exchange reports
`Invalid response from token exchange`, and means no policy matches every claim: compare the
policies with the table above, fix them and re-run.

The switch from the old `PULUMI_ACCESS_TOKEN` repository secret runs in this order:

1. Add the two policies above and set the `PULUMI_CI_USER` variable (the wizard's last
   stage does both) before the OIDC workflow reaches `master`; without the variable, the deploy
   fails at its first Pulumi step. The merge's own deploy is the validation.
2. Until a deploy through OIDC has succeeded, the rollback is to revert the workflow commit: the
   secret still works, so the next push deploys as before.
3. Check that no other workflow or tool uses the secret or the `sema-ci` token:
   `gh secret list`, and the token's last use under Pulumi Cloud → Personal access tokens.
4. Delete the secret (`gh secret delete PULUMI_ACCESS_TOKEN`) and revoke `sema-ci`.
5. Remove the saved copy: delete the `PULUMI_ACCESS_TOKEN=` line from
   `~/.config/sema/deploy.env`.

After that, the rollback for a broken trust is to fix the policy; a new personal access token set
as the secret would only work with the workflow reverted as well.

Signing in locally (`pulumi login`, for [deploys by hand](#deploy-by-hand)) is unchanged.

## Deploy by hand

This needs the one-time [Pulumi setup](#pulumi-setup) below. Sign in to AWS (`aws sso login`) and Pulumi Cloud (`pulumi login`), then from the repository root:
```bash
make test
make preview      # pulumi preview on untz/sema/prod
make deploy       # pulumi up on untz/sema/prod, then a CloudFront invalidation
```
`pulumi up` runs `make build` itself, with the Google client ID from the stack config, so the
Lambda ZIPs and the SPA always match the checkout. Review the preview before confirming the
update: the DynamoDB table, the content bucket, and the vector bucket hold user data, so none of
them should show as replaced or deleted.

The Lambda ZIPs are reproducible, so a function shows `~code` only when its binary changed. The
Makefile builds them with the `toolchain` version in `go.mod`, which CI installs, whatever Go is on
your `PATH`. It downloads that version into the Go module cache under `/tmp` whenever it is
missing, so the first build after a bump or a reboot needs the network.

`infra/go.mod` repeats the root `toolchain` line, so the Pulumi program and the infra tests never
build with an older Go than the Lambdas; unlike the Makefile, Go treats that line as a minimum, so
a newer Go on your `PATH` still wins there. Nothing checks that the two lines agree: bump them
together.

To target another organization's copy of the stack, pass `PULUMI_ORG=<org>`; every Make target
that reads stack outputs (`rescore`, `replay`, `redrive`, `redrive-deliveries`, the backfills) uses
the same stack.

## Rollback

Run the CI workflow on `master` with a `ref`, from GitHub (Actions → ci → Run workflow) or:
```bash
gh workflow run ci.yml --ref master -f ref=<known-good-commit>
```
Keep `--ref master`: the deploy job only runs for `master`, and `ref` picks what it tests and
deploys. Both jobs check out that ref, with the current workflow, so the ref must contain the CI
deploy job's prerequisites (`scripts/check-live.sh`, `scripts/lint-ci-files.sh`, the `/sema/` role
path); for an older commit, check it out and [deploy by hand](#deploy-by-hand). The next push to
`master` deploys `master` again, so a rollback is never permanent: revert or fix on `master` before
pushing. A branch name as `ref` is resolved once, by the test job, and that commit is what deploys.
GitHub keeps only one waiting run in the `deploy` concurrency group, so a push that queues behind a
waiting rollback replaces it: check the rollback's run actually deployed.

Config is not versioned with the code: a rollback deploys the old code with today's ESC values. If
it also needs an older config value, restore it from the environment's revision history before
running the workflow:
```bash
pulumi env version history untz/sema/prod
pulumi env get untz/sema/prod@<revision>        # compare, then pulumi env set the old value
```

## Pulumi setup

Production is one stack, `untz/sema/prod`, with its state in Pulumi Cloud and its secrets
encrypted by Pulumi Cloud; no passphrase is involved. Every config value, secrets included, lives
in the `untz/sema/prod` ESC environment, and `infra/Pulumi.prod.yaml` only imports it. The
Makefile targets this stack by default (`STACK=prod`, `PULUMI_ORG=untz`).

`bash scripts/setup-deploy.sh` walks the one-time move from the old local stack: Pulumi Cloud
login, a last local deploy so prod matches the checkout, the ESC environment built from the old
stack config, the state export with secrets and import into `untz/sema/prod`, and the gate: the
same config read back from ESC and a zero-change `pulumi preview`. Its next two stages apply the
[bootstrap stack](#bootstrap-stack) and move the Lambda roles under `/sema/`, and the last sets up
[CI deploys](#ci-deploys). It can be stopped and
re-run; its answers stay in `~/.config/sema/deploy.env`.

Each run overwrites `~/.config/sema/wizard.log` with what its screen shows, hidden answers
excepted. When helping with a stage, read the log instead of asking for a paste, with the
colour codes stripped: `sed 's/\x1b\[[0-9;?]*[A-Za-z]//g' ~/.config/sema/wizard.log | tail -80`.

The old local state (`~/.pulumi/stacks/sema/prod.json`) and its passphrase stack file are left
untouched as a backup; if the wizard had to deploy with the local stack first, the state from
before that update is also kept in `~/.config/sema/`. Never run `pulumi up` against the local
backend again: it would fork prod's state.

### Changing a config value

Edit the ESC environment, not the stack file: `pulumi config set` would write a stray copy into
`infra/Pulumi.prod.yaml`. Quote the property path, since keys contain `:`.
```bash
pulumi env set untz/sema/prod 'pulumiConfig["sema:domain"]' sema.example.com
pulumi env set untz/sema/prod 'pulumiConfig["sema:googleClientId"]' --secret <value>
pulumi env get untz/sema/prod 'pulumiConfig["sema:domain"]' --value string
pulumi env edit untz/sema/prod      # edit the whole definition
pulumi env version history untz/sema/prod
```
The change applies on the next `pulumi up`. Check what the stack reads with
`cd infra && pulumi config --stack untz/sema/prod`.

## Bootstrap stack

`infra/bootstrap` is the `sema-bootstrap` Pulumi project, stack `untz/sema-bootstrap/prod`. It owns
what CI deploys with: the `sema-lambda-boundary` permissions boundary, the `sema-github-deploy` role
CI assumes, and the role's inline policy, all built by `infra/deployaccess` (see
`docs/adr/0004-ci-deploys-through-github-oidc.md`). It imports the same `untz/sema/prod` ESC
environment as the main stack and reads `sema:githubSubjectPrefix` and `sema:route53ZoneId` from it.
`sema:githubSubjectPrefix` is the repository's OIDC subject prefix exactly as GitHub reports it
(`gh api repos/nuntz/sema/actions/oidc/customization/sub -q .sub_claim_prefix`). The repository
uses immutable subjects, so the prefix names the owner and repository by ID
(`repo:nuntz@261462/sema@1342395675`): a renamed or recreated repository can't assume the role.
Turning immutable subjects off, or renaming the repository, changes the prefix; update the value and
re-apply the bootstrap stack, or CI deploys fail with `Not authorized to perform
sts:AssumeRoleWithWebIdentity`.
Its `deployRoleArn` output is the `AWS_DEPLOY_ROLE_ARN` GitHub variable.

Only you apply it, by hand, with your own AWS session: CI's role is denied any change to itself and
to the boundary, so CI can never widen its own fence. `bash scripts/setup-deploy.sh` applies it the
first time, then runs the one local `pulumi up` of the main stack that moves the 7 Lambda roles
under `/sema/` with the boundary, which CI's role can't do. That move replaces each role
(create before delete), so each function errors for a few seconds and SQS retries absorb it. To
apply it by hand:
```bash
cd infra/bootstrap
pulumi stack select untz/sema-bootstrap/prod   # first time: pulumi stack init untz/sema-bootstrap/prod
pulumi preview
pulumi up
pulumi stack output deployRoleArn
```

Re-apply it before pushing a change that uses a new AWS service or gives a Lambda a new action:
`cd infra && go test ./...` fails until `deployaccess` grants the service to the deploy role or
allows the action in the boundary, and CI can't deploy that change until the bootstrap stack is
applied.

The account's GitHub OIDC provider is managed outside Sema's bootstrap stack, which only looks
it up. **Removing the existing provider breaks Sema's CI deploys** until the provider exists
again. Never create a second provider for Sema: the account allows only one per issuer URL.

## Operations

### Refresh rankings

The `rescore` Lambda re-ranks every live Item daily at 09:00 UTC. Run it on demand with:

```sh
make rescore
```

For an embedding or summary model upgrade, deploy the new configuration, replay the window, and then rescore:

```sh
cd infra
pulumi env set untz/sema/prod 'pulumiConfig["sema:modelVersion"]' amazon.titan-embed-text-v2:0
pulumi env set untz/sema/prod 'pulumiConfig["sema:summarizeModel"]' amazon.nova-micro-v1:0
pulumi up
cd ..
make replay MODEL_VERSION=amazon.titan-embed-text-v2:0
make replay FLAGS="--force-extract --force-summary"
make rescore
```

Replay reprocesses live Items, re-embedding text from the stored title, summary, and first paragraph when the vector is missing or belongs to another model version. It does not change Read state, Signals, Kept Items, or Archive bodies.

### Redrive failed queue messages

Move messages from the feeds and items dead-letter queues back to their source queues with:

```sh
make redrive
```

Use credentials for the target stack and review the queues before redriving production messages.
`make redrive` does not cover the deliveries dead-letter queue, because redriving it is
outward-facing; use `make redrive-deliveries`, below.

#### Deliveries

```sh
make redrive-deliveries
```

This moves the deliveries dead-letter queue back to the deliveries queue, where the delivery worker
picks each message up as if it were its next retry, and POSTs the Send to its user's Destination
unless a guard below cancels it. If that attempt fails in a retryable way, the message resumes its
retry schedule where it stopped.

A Send whose retries all fail does not land here: once the schedule is exhausted the delivery worker
records the result and SQS deletes the message. A message reaches the dead-letter queue after three
receives only when Sema itself failed:

- loading the Destination failed, so no POST was made;
- the POST failed in a retryable way and queueing the next retry then failed. A timeout counts as
  retryable, so the Destination may in fact have processed that POST;
- the delivery worker crashed or timed out.

These guards still apply to a redriven message:

- a removed or disabled Destination, or one whose URL has changed since the Send, cancels the
  delivery without a POST;
- each attempt is signed with the Destination's current secret and a fresh `X-Sema-Timestamp`;
- `X-Sema-Delivery` keeps the id from the original Send;
- the body is the one frozen when the user pressed Send, and its `sent_at` says when that was.

The remaining risks:

- the delivery is late: the dead-letter queue keeps messages for 14 days, so a redrive can POST a
  Send that old;
- a Destination that doesn't drop repeated `X-Sema-Delivery` ids may get a duplicate;
- the latest result shown in Settings is overwritten by the outcome of the old Send, even if the
  user has sent something newer since.

Before redriving, check how many messages are waiting and how old the oldest one is. The dashboard's
Dead letters chart shows the count; for both numbers from the command line:

```sh
dlq="$(cd infra && pulumi stack output deliveriesDlqArn --stack untz/sema/prod)"
aws sqs get-queue-attributes --region us-east-1 --attribute-names ApproximateNumberOfMessages \
  --queue-url "$(aws sqs get-queue-url --region us-east-1 --queue-name "${dlq##*:}" --query QueueUrl --output text)"
aws cloudwatch get-metric-statistics --region us-east-1 --namespace AWS/SQS \
  --metric-name ApproximateAgeOfOldestMessage --dimensions Name=QueueName,Value="${dlq##*:}" \
  --statistics Maximum --period 300 \
  --start-time "$(date -u -d '-15 minutes' +%FT%TZ)" --end-time "$(date -u +%FT%TZ)"
```

The age is in seconds and counts from when the message reached the dead-letter queue, a few minutes
after the Send; the `sent_at` in each message body is exact. If the oldest message is older than its
Destinations would still want, or you don't know why the messages failed, leave them in the queue to
expire instead of redriving.

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

Read writes use strongly consistent identity lookups and skip missing or expired Items by default. If an older stack needs to mark live Items Read before this backfill completes, temporarily set `LEGACY_READ_FALLBACK=true` on the API Lambda. This enables a consistent query through the user’s live partition when an identity is absent—including identities already removed by TTL—so leave it unset during normal operation and remove it after the final backfill pass. No deployment enables it by default.

### Split item-vector rollout and recovery

Deploy the split-vector writer and targeted rescore updates before migrating older live items. Then inspect and apply the DynamoDB item-vector backfill before allowing a scheduled or on-demand rescore:

```sh
make deploy STACK=prod
make backfill-item-vectors STACK=prod
make backfill-item-vectors STACK=prod BACKFILL_ARGS=--apply
```

The command is idempotent and dry-run by default. It creates only missing or expired `V#` rows, preferring a surviving inline embedding and otherwise restoring the original embedding from S3 Vectors. Investigate any non-zero `unavailable` count and replay those items before rescoring. The separate `backfill-vectors` command below copies DynamoDB embeddings in the opposite direction, into the S3 vector index.

### Permanent archive identities

Deploy the permanent archive-identity writer before running this migration.
It updates the existing `D#<item-id>` row to point at the timestamped `A#` key
and removes its TTL, so semantic search and similar results continue resolving
archives after the seven-day live window expires. Live key/expiry metadata keeps
hearted items available in the live window; unheart restores or removes the identity.

A hearted item keeps a permanent identity, so a feed that republishes it after
the live window is treated as a duplicate (`ItemsDeduped`) and does not re-enter
the live grid. Unhearting clears the identity after the live window has expired;
within that window, it restores the expiring live identity.

```sh
make deploy STACK=prod
make backfill-archive-identities STACK=prod
make backfill-archive-identities STACK=prod BACKFILL_ARGS=--apply
```

The command is dry-run by default and idempotent. It lists archive rows only in
this maintenance path, preserves existing live identity metadata, and checks that
the archive and identity have not changed before writing. Repeat the apply pass
if concurrent changes caused a row to be skipped. No key-schema, GSI, or IAM
changes are required. Existing archives need this backfill to regain resolution.

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

By default, replay only re-embeds text when the stored vector is missing or belongs to a different `MODEL_VERSION`; pass `FLAGS="--force-extract"` to force a fresh text embedding.

### User-scoped vector migration

Vector keys now include the account ID and queries filter `user_id` before
selecting neighbours. Old global records cannot serve these readers: switching
readers before backfilling leaves search incomplete and similarity can return
404. Migrate both indexes before deploying the new readers.

1. Build the updated application and maintenance tools with `make build`.
   Preview the intended stack explicitly with `cd infra && pulumi preview --stack prod`.
2. Run the new backfill writer in dry-run mode against both indexes:

   ```sh
   make backfill-vectors STACK=prod
   make backfill-vectors STACK=prod BACKFILL_ARGS='--image'
   ```

3. During a maintenance window, disable the item-worker SQS event-source mapping
   and temporarily set API reserved concurrency to zero to prevent heart changes.
   Record their original settings first. Once the mapping reports `Disabled`,
   allow at least 120 seconds for existing item-worker and API invocations to finish.
   Pause any manual replay or migration tools as well.
4. Populate both indexes using the new writer while the existing application
   readers remain deployed:

   ```sh
   make backfill-vectors STACK=prod BACKFILL_ARGS='--apply'
   make backfill-vectors STACK=prod BACKFILL_ARGS='--image --apply'
   ```

   Both commands must succeed before switching readers. They reuse stored
   embeddings; archive retention wins when a live and archive row share an ID.
   Missing stored embeddings require recovery before they can be indexed.
5. Deploy with `cd infra && pulumi up --stack prod`, then restore the recorded
   API concurrency setting and event-source mapping state. Remove the temporary
   concurrency limit if the API previously had no reserved limit. Verify search,
   similarity, and archive actions for each account.
6. After verifying the migration and retiring all old writers, inspect and
   optionally purge obsolete global records:

   ```sh
   make purge-legacy-vectors STACK=prod
   make purge-legacy-vectors STACK=prod BACKFILL_ARGS='--image'
   # After reviewing the dry-run counts:
   make purge-legacy-vectors STACK=prod BACKFILL_ARGS='--apply'
   make purge-legacy-vectors STACK=prod BACKFILL_ARGS='--image --apply'
   ```

The purge is idempotent and dry-run by default. It deletes only records without
`user_id` metadata, including global archive records that ordinary expiry
cleanup retains forever. Namespaced records remain untouched. Purging ends the
legacy S3 fallback used by `backfill-item-vectors`; finish any required recovery
and retain old records if rollback to global readers is still needed.

An indexing outage leaves ingested items in DynamoDB and retries the SQS
messages using stored embeddings. The queue moves messages to its DLQ after
three receives; a prolonged outage still needs DLQ redrive after recovery or a
vector backfill. Run redrive against the intended stack with
`make redrive STACK=prod`.

### Image ranking rollout

Deploy the image index and writer before replaying live items or changing existing preference models:

```sh
make deploy STACK=prod
make replay STACK=prod
make backfill-image-signals STACK=prod
make backfill-image-signals STACK=prod BACKFILL_ARGS=--apply
make rescore STACK=prod
```

The first command creates the `images` vector index and enables image writes. Replay embeds lead images for live, non-video items without re-embedding text. Inspect the dry-run backfill counts before applying it to give existing hearts and behaviour rows image vectors. Run rescore after the backfill, or wait for the nightly run; the first rescore after rollout will visibly reshuffle item sizes as the new image centroids take effect.

The image-search similarity floor is a tuning knob for text-to-image recall. `ImageSearchTopSimilarity` and `ImageSearchMatches` in the Sema CloudWatch namespace show the live score distribution; adjust the floor with `pulumi env set untz/sema/prod 'pulumiConfig["sema:imageSearchMinSimilarity"]' <n>` before deploying the stack.

### YouTube connector rollout

For deployments that predate the YouTube connector, gate discovery while migrating stored channel feeds:

```sh
cd infra
pulumi env set untz/sema/prod 'pulumiConfig["sema:youtubeDiscoveryEnabled"]' false
cd ..
make deploy
make backfill-youtube-connector STACK=prod
make backfill-youtube-connector STACK=prod BACKFILL_ARGS=--apply
cd infra
pulumi env set untz/sema/prod 'pulumiConfig["sema:youtubeDiscoveryEnabled"]' true
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

### Send rollout

Send adds the `deliveries` queue with its dead-letter queue and alarm, the `delivery-worker` Lambda, and the `DELIVERIES_QUEUE_URL` variable on the API. `make deploy` creates all of them; no configuration is required. Items start recording the origin URL of their lead image on ingest or replay, and there is no backfill; every Item with a stored lead image also sends a one-hour signed URL to a per-Send copy of Sema's image under `send/`, which a lifecycle rule deletes after a day. That copy needs the `sema:domain` custom domain, which becomes the API's `PUBLIC_ORIGIN`; without one, Items ingested before the rollout send an empty `images` list.
