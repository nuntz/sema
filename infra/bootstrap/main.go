// Command bootstrap is the sema-bootstrap Pulumi program: what CI deploys Sema
// with. It owns the Lambda permissions boundary and the deploy role, and is
// applied only by hand with a personal AWS session (RUNBOOK.md → Bootstrap
// stack), so CI can never change its own permissions.
package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nuntz/sema/infra/deployaccess"
	awsprovider "github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Reads the main stack's ESC environment, so the repository and zone are
		// edited in one place.
		cfg := config.New(ctx, "sema")
		repository := strings.TrimSpace(cfg.Require("githubRepository"))
		hostedZoneID := strings.TrimSpace(cfg.Require("route53ZoneId"))

		identity, err := awsprovider.GetCallerIdentity(ctx, &awsprovider.GetCallerIdentityArgs{})
		if err != nil {
			return fmt.Errorf("look up AWS account: %w", err)
		}
		// The account has one GitHub OIDC provider, owned by Stash's bootstrap
		// stack; Sema only looks it up (ADR 0004).
		provider, err := iam.LookupOpenIdConnectProvider(ctx, &iam.LookupOpenIdConnectProviderArgs{Url: pulumi.StringRef(deployaccess.GitHubOIDCURL)})
		if err != nil {
			return fmt.Errorf("look up the GitHub OIDC provider (Stash's bootstrap stack owns it): %w", err)
		}
		if !slices.Contains(provider.ClientIdLists, deployaccess.Audience) {
			return fmt.Errorf("GitHub OIDC provider %s does not accept the %s audience", provider.Arn, deployaccess.Audience)
		}

		boundary, err := deployaccess.BoundaryPolicy(identity.AccountId).JSON()
		if err != nil {
			return err
		}
		trust, err := deployaccess.TrustPolicy(provider.Arn, repository).JSON()
		if err != nil {
			return err
		}
		access, err := deployaccess.DeployPolicy(identity.AccountId, hostedZoneID).JSON()
		if err != nil {
			return err
		}

		// No path: the main stack refers to the boundary by deployaccess.BoundaryARN.
		if _, err := iam.NewPolicy(ctx, "lambda-boundary", &iam.PolicyArgs{
			Name:        pulumi.String(deployaccess.BoundaryName),
			Description: pulumi.String("The most any Sema Lambda role may do; the CI deploy role must attach it."),
			Policy:      pulumi.String(boundary),
		}); err != nil {
			return err
		}
		role, err := iam.NewRole(ctx, "github-deploy", &iam.RoleArgs{
			Name:             pulumi.String(deployaccess.DeployRoleName),
			Description:      pulumi.String("Assumed by the GitHub Actions deploy job of the production environment."),
			AssumeRolePolicy: pulumi.String(trust),
		})
		if err != nil {
			return err
		}
		if _, err := iam.NewRolePolicy(ctx, "github-deploy-access", &iam.RolePolicyArgs{
			Role:   role.ID(),
			Policy: pulumi.String(access),
		}); err != nil {
			return err
		}

		// The AWS_DEPLOY_ROLE_ARN GitHub variable.
		ctx.Export("deployRoleArn", role.Arn)
		return nil
	})
}
