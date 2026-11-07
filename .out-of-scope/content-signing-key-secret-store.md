# Moving the content-signing key into a secret store

Sema keeps passing the CloudFront content-signing private key to the API Lambda as the `CF_PRIVATE_KEY` environment variable. It doesn't move the key into SSM Parameter Store, Secrets Manager, or an environment variable encrypted with a customer-managed KMS key.

## What the key protects

The API uses the key to sign CloudFront cookies for `/bodies`, `/media`, `/archive` and `/send`. Anyone holding the key can mint cookies for any user's private content until the key is rotated. It grants no write access and no API access.

## Why this is out of scope

A secret store only changes where the Lambda reads the key from. It doesn't change who can read it:

- **Pulumi generates the key** (`tls.PrivateKey`) and keeps it in encrypted stack state, and CI decrypts that state on every deploy. Moving the key into a secret store still leaves it in state, because Pulumi writes the secret's value.
- **The deploy role can deploy code to every `sema-*` function** (`lambda:*`, ADR 0004). Any principal that can deploy the API can deploy code that reads the key, wherever it's stored.

Moving the key would only remove access for principals who can read a Lambda's configuration but can't deploy to it, such as read-only or console roles in the AWS account. Sema shares its account with another app, but its maintainer is the only person with access to it. So no such principal exists, and the change would protect nothing.

The change would still have costs. Every option widens the deploy role and the Lambda boundary, which AGENTS.md treats as a fence that should change only on purpose. It also requires a hand-applied bootstrap stack and adds a secret fetch at cold start.

## When to reconsider

Reopen this if someone other than the maintainer gains read access to Lambda configuration in the account. If that happens, the cheapest fix is an SSM SecureString with the AWS-managed `aws/ssm` key, under a `/sema/<stack>/` path, read by the API at cold start. The boundary would gain `ssm:GetParameter`, and the deploy role would gain `ssm` limited to that path.

A real reduction of the key's exposure would mean generating it outside Pulumi, so it never enters stack state. That's a different and bigger change than any of these storage options.

## Prior requests

- #15: "Harden content-signing key handling"
