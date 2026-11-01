// Package deployaccess is the single source of Sema's CI trust model (see
// docs/adr/0004-ci-deploys-through-github-oidc.md): who may assume the deploy
// role, what that role may do, and the boundary that caps every Lambda role it
// deploys.
package deployaccess

import (
	"fmt"
	"slices"
)

const (
	// RolePath is the IAM path of every role the deploy role may manage.
	RolePath = "/sema/"
	// BoundaryName names the permissions boundary every such role must carry.
	BoundaryName = "sema-lambda-boundary"
	// DeployRoleName names the role CI assumes. It sits outside RolePath so it
	// can never manage itself.
	DeployRoleName = "sema-github-deploy"
	// Environment is the only GitHub environment that may assume the deploy role.
	Environment = "production"
	// Audience is the OIDC audience GitHub's token must carry.
	Audience = "sts.amazonaws.com"

	githubIssuer = "token.actions.githubusercontent.com"
)

// TrustPolicy lets only repository's Environment assume the role through the
// GitHub OIDC provider at providerARN.
func TrustPolicy(providerARN, repository string) Document {
	return Document{Version: "2012-10-17", Statement: []Statement{{
		Sid:       "GitHubProductionEnvironment",
		Effect:    "Allow",
		Principal: map[string]string{"Federated": providerARN},
		Action:    []string{"sts:AssumeRoleWithWebIdentity"},
		Condition: map[string]map[string]string{"StringEquals": {
			githubIssuer + ":aud": Audience,
			githubIssuer + ":sub": fmt.Sprintf("repo:%s:environment:%s", repository, Environment),
		}},
	}}}
}

// accountWideNamespaces are the services the main stack uses that the deploy
// role manages without a resource fence. Sema shares the account with Stash,
// and these grants reach Stash's resources too: the boundary, not a name
// prefix, is what stops escalation (see ADR 0004).
var accountWideNamespaces = []string{
	"acm", "apigateway", "cloudfront", "cloudwatch", "dynamodb", "events",
	"logs", "s3", "s3vectors", "sns", "sqs",
}

// fencedNamespaces are granted only on Sema's own resources. Lambda is fenced
// because new code in another app's function would run as that app's
// unbounded role.
var fencedNamespaces = []string{"iam", "lambda", "route53"}

// FunctionPrefix starts the name of every Lambda function the deploy role may
// manage; the main stack names them sema-<stack>-<name>.
const FunctionPrefix = "sema-"

// ServiceNamespaces lists every IAM service namespace the deploy role is
// granted, sorted.
func ServiceNamespaces() []string {
	namespaces := slices.Concat(accountWideNamespaces, fencedNamespaces)
	slices.Sort(namespaces)
	return namespaces
}

// BoundaryARN is the boundary policy's ARN in accountID.
func BoundaryARN(accountID string) string {
	return fmt.Sprintf("arn:aws:iam::%s:policy/%s", accountID, BoundaryName)
}

// DeployRoleARN is the deploy role's ARN in accountID.
func DeployRoleARN(accountID string) string {
	return fmt.Sprintf("arn:aws:iam::%s:role/%s", accountID, DeployRoleName)
}

func roleARNs(accountID string) []string {
	return []string{fmt.Sprintf("arn:aws:iam::%s:role%s*", accountID, RolePath)}
}

// DeployPolicy is the deploy role's policy in accountID, with Route 53 changes
// limited to hostedZoneID.
func DeployPolicy(accountID, hostedZoneID string) Document {
	bounded := map[string]map[string]string{"StringEquals": {"iam:PermissionsBoundary": BoundaryARN(accountID)}}
	services := make([]string, len(accountWideNamespaces))
	for index, namespace := range accountWideNamespaces {
		services[index] = namespace + ":*"
	}
	zone := "arn:aws:route53:::hostedzone/" + hostedZoneID
	functions := fmt.Sprintf("arn:aws:lambda:*:%s:function:%s*", accountID, FunctionPrefix)
	return Document{Version: "2012-10-17", Statement: []Statement{
		{
			Sid:      "ManageSemaServices",
			Effect:   "Allow",
			Action:   services,
			Resource: []string{"*"},
		},
		{
			Sid:      "ManageSemaFunctions",
			Effect:   "Allow",
			Action:   []string{"lambda:*"},
			Resource: []string{functions},
		},
		// Event source mappings have their own ARNs; AWS names the function a
		// mapping invokes in lambda:FunctionArn.
		{
			Sid:       "MapQueuesToSemaFunctions",
			Effect:    "Allow",
			Action:    []string{"lambda:CreateEventSourceMapping", "lambda:UpdateEventSourceMapping", "lambda:DeleteEventSourceMapping"},
			Resource:  []string{"*"},
			Condition: map[string]map[string]string{"ArnLike": {"lambda:FunctionArn": functions}},
		},
		{
			Sid:      "ReadLambdaForRefresh",
			Effect:   "Allow",
			Action:   []string{"lambda:GetEventSourceMapping", "lambda:ListEventSourceMappings", "lambda:ListFunctions", "lambda:ListTags", "lambda:GetAccountSettings"},
			Resource: []string{"*"},
		},
		{
			Sid:       "CreateBoundedSemaRoles",
			Effect:    "Allow",
			Action:    []string{"iam:CreateRole", "iam:PutRolePermissionsBoundary"},
			Resource:  roleARNs(accountID),
			Condition: bounded,
		},
		{
			Sid:    "ManageSemaRoles",
			Effect: "Allow",
			Action: []string{
				"iam:GetRole", "iam:DeleteRole", "iam:UpdateRole", "iam:UpdateRoleDescription", "iam:UpdateAssumeRolePolicy",
				"iam:TagRole", "iam:UntagRole", "iam:ListRoleTags", "iam:ListInstanceProfilesForRole",
				"iam:PutRolePolicy", "iam:GetRolePolicy", "iam:DeleteRolePolicy", "iam:ListRolePolicies", "iam:ListAttachedRolePolicies",
			},
			Resource: roleARNs(accountID),
		},
		{
			Sid:       "PassSemaRolesToLambda",
			Effect:    "Allow",
			Action:    []string{"iam:PassRole"},
			Resource:  roleARNs(accountID),
			Condition: map[string]map[string]string{"StringEquals": {"iam:PassedToService": "lambda.amazonaws.com"}},
		},
		{
			Sid:      "ManageSemaRecords",
			Effect:   "Allow",
			Action:   []string{"route53:ChangeResourceRecordSets", "route53:ListResourceRecordSets", "route53:GetHostedZone", "route53:ListTagsForResource"},
			Resource: []string{zone},
		},
		// Pulumi waits on record changes after an update and reads them on refresh.
		{
			Sid:      "ReadRecordChanges",
			Effect:   "Allow",
			Action:   []string{"route53:GetChange"},
			Resource: []string{"arn:aws:route53:::change/*"},
		},
		// Explicit denies win over any Allow, so the fence holds even if another
		// statement or policy on the role is ever widened.
		{
			Sid:      "KeepTheBoundary",
			Effect:   "Deny",
			Action:   []string{"iam:CreatePolicy", "iam:CreatePolicyVersion", "iam:DeletePolicy", "iam:DeletePolicyVersion", "iam:SetDefaultPolicyVersion", "iam:TagPolicy", "iam:UntagPolicy"},
			Resource: []string{BoundaryARN(accountID)},
		},
		{
			Sid:      "KeepBoundariesOnRoles",
			Effect:   "Deny",
			Action:   []string{"iam:DeleteRolePermissionsBoundary"},
			Resource: []string{"*"},
		},
		{
			Sid:      "LeaveTheDeployRoleAlone",
			Effect:   "Deny",
			Action:   []string{"iam:*"},
			Resource: []string{DeployRoleARN(accountID)},
		},
	}}
}

// BoundaryPolicy is the most any Sema Lambda role may do: read and write data
// in the stack's services, never manage them, and nothing in IAM or STS. A role
// policy that grants more is silently capped, so the main stack's tests check
// every Lambda action against it.
func BoundaryPolicy() Document {
	return Document{Version: "2012-10-17", Statement: []Statement{{
		Sid:    "SemaLambdaDataPlane",
		Effect: "Allow",
		Action: []string{
			"logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents",
			"dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:DeleteItem", "dynamodb:ConditionCheckItem",
			"dynamodb:Query", "dynamodb:Scan", "dynamodb:BatchGetItem", "dynamodb:BatchWriteItem", "dynamodb:TransactGetItems", "dynamodb:TransactWriteItems",
			"s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket",
			"sqs:SendMessage", "sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:ChangeMessageVisibility", "sqs:GetQueueAttributes",
			"s3vectors:PutVectors", "s3vectors:GetVectors", "s3vectors:QueryVectors", "s3vectors:ListVectors", "s3vectors:DeleteVectors",
			"bedrock:InvokeModel",
			"lambda:InvokeFunction",
		},
		Resource: []string{"*"},
	}}}
}
