# Sema Operations Runbook

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

To target another organization's copy of the stack, pass `PULUMI_ORG=<org>`; every Make target
that reads stack outputs (`rescore`, `replay`, `redrive`, the backfills) uses the same stack.

## Pulumi setup

Production is one stack, `untz/sema/prod`, with its state in Pulumi Cloud and its secrets
encrypted by Pulumi Cloud; no passphrase is involved. Every config value, secrets included, lives
in the `untz/sema/prod` ESC environment, and `infra/Pulumi.prod.yaml` only imports it. The
Makefile targets this stack by default (`STACK=prod`, `PULUMI_ORG=untz`).

`bash scripts/setup-deploy.sh` walks the one-time move from the old local stack: Pulumi Cloud
login, a last local deploy so prod matches the checkout, the ESC environment built from the old
stack config, the state export with secrets and import into `untz/sema/prod`, and the gate: the
same config read back from ESC and a zero-change `pulumi preview`. Its last two stages apply the
[bootstrap stack](#bootstrap-stack) and move the Lambda roles under `/sema/`. It can be stopped and
re-run; its answers stay in `~/.config/sema/deploy.env`.

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
environment as the main stack and reads `sema:githubRepository` and `sema:route53ZoneId` from it.
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
