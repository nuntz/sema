# CI deploys through GitHub OIDC, fenced by a role path and boundary

Every green push to `master` deploys `untz/sema/prod` from GitHub Actions. The job reaches AWS as `sema-github-deploy`, a role only this repository's `production` environment can assume, so no long-lived AWS key exists. Sema shares its AWS account with another app, and the repository is public, so the role's reach is fenced rather than trusted: it may manage Sema's services account-wide, but Lambda only on functions named `sema-*`, IAM only on roles under `/sema/` that carry the `sema-lambda-boundary` permissions boundary, and Route 53 only in Sema's hosted zone. Nothing it deploys can be granted more than the boundary allows, including itself. IAM can't fence a role's trust policy, so the boundary also stops a role working outside a `sema-*` function in the account, except for the queue polling that event source mappings do.

## Considered Options

- **Scoping each service by a `sema-` name prefix.** Rejected: the DynamoDB table and SQS queues carry generated names, and renaming the table replaces it and loses its data. The boundary already stops escalation, which is the risk prefixes would guard against. Lambda is the exception: new code in another app's function would run as that app's unbounded role, and Sema's functions already carry the prefix.
- **Owning the GitHub OIDC provider.** An account holds one provider per issuer URL, and the existing provider is managed outside Sema's bootstrap stack. Sema's bootstrap stack looks it up instead of creating, importing, or moving it into a shared stack.

## Consequences

- Removing the existing GitHub OIDC provider breaks Sema's CI deploys until the provider exists again.
- The bootstrap stack (`infra/bootstrap`) owns the role and the boundary and is applied only by hand with a personal AWS session. A change that needs a new AWS action fails the `deployaccess` test until the policy grants it, and CI cannot deploy it until the bootstrap stack is applied.
- Moving the Lambda roles under `/sema/` replaces them; that one `pulumi up` runs locally, because CI's role cannot manage roles outside the path.
- CI reaches Pulumi Cloud through GitHub OIDC as well, trusting the same `production` environment subject, so GitHub stores no Pulumi token either. Pulumi's free tier issues only personal OIDC tokens, so the job's one-hour token carries all of `PULUMI_OIDC_USER`'s Pulumi access, not just this stack's (RUNBOOK.md → Pulumi Cloud sign-in).
