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

The job runs in the `production` GitHub environment, which GitHub limits to `master`, with
`id-token: write`, in one `deploy` concurrency group that never cancels a run in progress. It
reaches AWS through GitHub OIDC as `sema-github-deploy`, so no AWS key is stored in GitHub; the
role and its fence are the [bootstrap stack](#bootstrap-stack)'s, and
`docs/adr/0004-ci-deploys-through-github-oidc.md` records the trust model.

GitHub holds the `PULUMI_ACCESS_TOKEN` repository secret and three repository variables:
`PULUMI_STACK` (`untz/sema/prod`), `PULUMI_ESC_ENVIRONMENT` (`sema/prod`) and `AWS_DEPLOY_ROLE_ARN`
(the bootstrap stack's `deployRoleArn` output). They are repository variables, not `production`
environment ones, because the job decides whether to run from `PULUMI_STACK` before it enters the
environment: an environment variable would always skip it. Until `PULUMI_STACK` is set, and on
forks, the job is skipped without failing. The last stage of `bash scripts/setup-deploy.sh` creates
the environment and sets the secret and variables.

When a deploy fails with `AccessDenied`, the message names the IAM action the deploy role lacks:
grant it in `infra/deployaccess`, apply the bootstrap stack by hand, then re-run the workflow.

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

The Lambda ZIPs are reproducible, so a function shows `~code` only when its binary changed. If all
seven show `~code` after a change that touched no Go code, your local Go is not the `toolchain`
version in `go.mod`, which CI builds with, and the update would restart every function for nothing
(#9). Build with `GOTOOLCHAIN=<that version> make deploy` until the Makefile pins it.

To target another organization's copy of the stack, pass `PULUMI_ORG=<org>`; every Make target
that reads stack outputs (`rescore`, `replay`, `redrive`, the backfills) uses the same stack.

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

The account has one GitHub OIDC provider, and Stash's bootstrap stack (`untz/stash-bootstrap/prod`)
owns it; Sema's only looks it up. **Destroying Stash's bootstrap stack, or removing the provider
from it, breaks Sema's CI deploys** until the provider exists again. Never create a second provider
for Sema: the account allows only one per issuer URL.
