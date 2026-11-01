package deployaccess

import (
	"slices"
	"testing"
)

const (
	testAccount  = "123456789012"
	testProvider = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
	testZone     = "Z0123456789ABC"
)

func assumeWith(subject, audience string) Request {
	return Request{
		Action:    "sts:AssumeRoleWithWebIdentity",
		Principal: testProvider,
		Context: map[string]string{
			"token.actions.githubusercontent.com:sub": subject,
			"token.actions.githubusercontent.com:aud": audience,
		},
	}
}

func TestTrustPolicyAdmitsOnlyTheProductionEnvironment(t *testing.T) {
	trust := TrustPolicy(testProvider, "nuntz/sema")
	if !trust.Allows(assumeWith("repo:nuntz/sema:environment:production", "sts.amazonaws.com")) {
		t.Fatal("production environment cannot assume the deploy role")
	}
	for name, request := range map[string]Request{
		"master branch":     assumeWith("repo:nuntz/sema:ref:refs/heads/master", "sts.amazonaws.com"),
		"pull request":      assumeWith("repo:nuntz/sema:pull_request", "sts.amazonaws.com"),
		"other environment": assumeWith("repo:nuntz/sema:environment:staging", "sts.amazonaws.com"),
		"other repository":  assumeWith("repo:nuntz/stash:environment:production", "sts.amazonaws.com"),
		"fork lookalike":    assumeWith("repo:nuntz/sema-fork:environment:production", "sts.amazonaws.com"),
		"wrong audience":    assumeWith("repo:nuntz/sema:environment:production", "https://github.com/nuntz"),
		"missing audience":  {Action: "sts:AssumeRoleWithWebIdentity", Principal: testProvider, Context: map[string]string{"token.actions.githubusercontent.com:sub": "repo:nuntz/sema:environment:production"}},
		"other provider":    {Action: "sts:AssumeRoleWithWebIdentity", Principal: "arn:aws:iam::123456789012:oidc-provider/example.com", Context: assumeWith("repo:nuntz/sema:environment:production", "sts.amazonaws.com").Context},
		"plain assume role": {Action: "sts:AssumeRole", Principal: testProvider, Context: assumeWith("repo:nuntz/sema:environment:production", "sts.amazonaws.com").Context},
	} {
		if trust.Allows(request) {
			t.Errorf("%s can assume the deploy role", name)
		}
	}
}

func TestBoundaryIsNamedAndAddressedForTheAccount(t *testing.T) {
	if got, want := BoundaryARN(testAccount), "arn:aws:iam::123456789012:policy/sema-lambda-boundary"; got != want {
		t.Fatalf("BoundaryARN = %q, want %q", got, want)
	}
	if RolePath != "/sema/" || BoundaryName != "sema-lambda-boundary" {
		t.Fatalf("RolePath = %q, BoundaryName = %q", RolePath, BoundaryName)
	}
}

func TestDeployRoleManagesOnlyBoundedSemaRoles(t *testing.T) {
	policy := DeployPolicy(testAccount, testZone)
	semaRole := "arn:aws:iam::123456789012:role/sema/api-role-abc123"
	bounded := map[string]string{"iam:PermissionsBoundary": BoundaryARN(testAccount)}
	for _, action := range []string{"iam:CreateRole", "iam:PutRolePermissionsBoundary"} {
		if !policy.Allows(Request{Action: action, Resource: semaRole, Context: bounded}) {
			t.Errorf("%s with the boundary is denied on a /sema/ role", action)
		}
		if policy.Allows(Request{Action: action, Resource: semaRole}) {
			t.Errorf("%s without a boundary is allowed", action)
		}
		if policy.Allows(Request{Action: action, Resource: semaRole, Context: map[string]string{"iam:PermissionsBoundary": "arn:aws:iam::aws:policy/AdministratorAccess"}}) {
			t.Errorf("%s with another boundary is allowed", action)
		}
		for _, outside := range []string{"arn:aws:iam::123456789012:role/stash-api", "arn:aws:iam::123456789012:role/stash/api", "arn:aws:iam::123456789012:role/sema-admin"} {
			if policy.Allows(Request{Action: action, Resource: outside, Context: bounded}) {
				t.Errorf("%s is allowed on %s", action, outside)
			}
		}
	}
	for _, action := range []string{"iam:GetRole", "iam:DeleteRole", "iam:UpdateAssumeRolePolicy", "iam:PutRolePolicy", "iam:GetRolePolicy", "iam:DeleteRolePolicy", "iam:ListRolePolicies", "iam:ListAttachedRolePolicies"} {
		if !policy.Allows(Request{Action: action, Resource: semaRole}) {
			t.Errorf("%s is denied on a /sema/ role", action)
		}
		if policy.Allows(Request{Action: action, Resource: "arn:aws:iam::123456789012:role/stash-api"}) {
			t.Errorf("%s is allowed outside /sema/", action)
		}
	}
	for _, action := range []string{"iam:DeleteRolePermissionsBoundary", "iam:CreateUser", "iam:CreatePolicy", "iam:AttachRolePolicy"} {
		if policy.Allows(Request{Action: action, Resource: semaRole, Context: bounded}) {
			t.Errorf("%s is allowed", action)
		}
	}
	toLambda := map[string]string{"iam:PassedToService": "lambda.amazonaws.com"}
	if !policy.Allows(Request{Action: "iam:PassRole", Resource: semaRole, Context: toLambda}) {
		t.Error("cannot pass a /sema/ role to Lambda")
	}
	if policy.Allows(Request{Action: "iam:PassRole", Resource: semaRole, Context: map[string]string{"iam:PassedToService": "ec2.amazonaws.com"}}) {
		t.Error("can pass a /sema/ role to EC2")
	}
	if policy.Allows(Request{Action: "iam:PassRole", Resource: "arn:aws:iam::123456789012:role/stash-api", Context: toLambda}) {
		t.Error("can pass a role outside /sema/")
	}
}

func TestDeployRoleCannotWidenItsOwnFence(t *testing.T) {
	if got, want := DeployRoleARN(testAccount), "arn:aws:iam::123456789012:role/sema-github-deploy"; got != want {
		t.Fatalf("DeployRoleARN = %q, want %q", got, want)
	}
	policy := DeployPolicy(testAccount, testZone)
	bounded := map[string]string{"iam:PermissionsBoundary": BoundaryARN(testAccount)}
	for _, action := range []string{"iam:CreatePolicy", "iam:CreatePolicyVersion", "iam:DeletePolicyVersion", "iam:SetDefaultPolicyVersion", "iam:DeletePolicy", "iam:TagPolicy"} {
		if policy.Allows(Request{Action: action, Resource: BoundaryARN(testAccount)}) {
			t.Errorf("%s is allowed on the boundary", action)
		}
	}
	for _, action := range []string{"iam:PutRolePolicy", "iam:DeleteRolePolicy", "iam:AttachRolePolicy", "iam:UpdateAssumeRolePolicy", "iam:DeleteRole", "iam:PutRolePermissionsBoundary", "iam:DeleteRolePermissionsBoundary", "iam:TagRole"} {
		if policy.Allows(Request{Action: action, Resource: DeployRoleARN(testAccount), Context: bounded}) {
			t.Errorf("%s is allowed on the deploy role", action)
		}
	}
	// An explicit deny wins over any other policy on the role, so the fence
	// holds even if an Allow is ever widened.
	widened := Document{Statement: append(append([]Statement{}, policy.Statement...), Statement{Effect: "Allow", Action: []string{"iam:*"}, Resource: []string{"*"}})}
	for _, request := range []Request{
		{Action: "iam:CreatePolicyVersion", Resource: BoundaryARN(testAccount)},
		{Action: "iam:DeletePolicy", Resource: BoundaryARN(testAccount)},
		{Action: "iam:PutRolePolicy", Resource: DeployRoleARN(testAccount)},
		{Action: "iam:AttachRolePolicy", Resource: DeployRoleARN(testAccount)},
		{Action: "iam:UpdateAssumeRolePolicy", Resource: DeployRoleARN(testAccount)},
		{Action: "iam:DeleteRolePermissionsBoundary", Resource: "arn:aws:iam::123456789012:role/sema/api-role-abc123"},
	} {
		if widened.Allows(request) {
			t.Errorf("%s on %s is not explicitly denied", request.Action, request.Resource)
		}
	}
}

func TestDeployRoleChangesRecordsOnlyInSemasZone(t *testing.T) {
	policy := DeployPolicy(testAccount, testZone)
	zone := "arn:aws:route53:::hostedzone/" + testZone
	for _, action := range []string{"route53:ChangeResourceRecordSets", "route53:ListResourceRecordSets", "route53:GetHostedZone"} {
		if !policy.Allows(Request{Action: action, Resource: zone}) {
			t.Errorf("%s is denied in Sema's zone", action)
		}
		if policy.Allows(Request{Action: action, Resource: "arn:aws:route53:::hostedzone/ZSTASH"}) {
			t.Errorf("%s is allowed in another zone", action)
		}
	}
	if !policy.Allows(Request{Action: "route53:GetChange", Resource: "arn:aws:route53:::change/C123"}) {
		t.Error("cannot wait for a record change")
	}
	for _, action := range []string{"route53:CreateHostedZone", "route53:DeleteHostedZone", "route53domains:TransferDomain"} {
		if policy.Allows(Request{Action: action, Resource: "*"}) {
			t.Errorf("%s is allowed", action)
		}
	}
}

func TestDeployRoleGrantsItsServiceNamespacesAccountWide(t *testing.T) {
	policy := DeployPolicy(testAccount, testZone)
	namespaces := ServiceNamespaces()
	for _, want := range []string{"dynamodb", "lambda", "sqs", "s3", "cloudfront", "iam", "route53"} {
		if !slices.Contains(namespaces, want) {
			t.Errorf("ServiceNamespaces lacks %s", want)
		}
	}
	for _, namespace := range namespaces {
		if namespace == "iam" || namespace == "route53" || namespace == "lambda" {
			continue
		}
		resource := "arn:aws:" + namespace + ":us-east-1:123456789012:stash-thing"
		if !policy.Allows(Request{Action: namespace + ":DeleteAnything", Resource: resource}) {
			t.Errorf("%s is not granted account-wide", namespace)
		}
	}
	for _, action := range []string{"ec2:RunInstances", "organizations:LeaveOrganization", "sts:AssumeRole", "kms:ScheduleKeyDeletion"} {
		if policy.Allows(Request{Action: action, Resource: "*"}) {
			t.Errorf("%s is allowed", action)
		}
	}
}

func TestBoundaryCapsLambdaRolesAtTheDataPlane(t *testing.T) {
	boundary := BoundaryPolicy()
	for _, action := range []string{"dynamodb:Query", "dynamodb:TransactWriteItems", "s3:GetObject", "sqs:ReceiveMessage", "s3vectors:QueryVectors", "bedrock:InvokeModel", "logs:PutLogEvents", "lambda:InvokeFunction"} {
		if !boundary.Allows(Request{Action: action, Resource: "arn:aws:mock:::resource"}) {
			t.Errorf("boundary denies %s", action)
		}
	}
	for _, action := range []string{"iam:CreateRole", "iam:PassRole", "iam:PutRolePolicy", "sts:AssumeRole", "dynamodb:DeleteTable", "s3:DeleteBucket", "s3:PutBucketPolicy", "sqs:DeleteQueue", "lambda:UpdateFunctionCode", "lambda:CreateFunction", "s3vectors:DeleteIndex", "ec2:RunInstances"} {
		if boundary.Allows(Request{Action: action, Resource: "*"}) {
			t.Errorf("boundary allows %s", action)
		}
	}
}

func TestDeployRoleManagesOnlySemaFunctions(t *testing.T) {
	policy := DeployPolicy(testAccount, testZone)
	sema := "arn:aws:lambda:us-east-1:123456789012:function:sema-prod-api"
	stash := "arn:aws:lambda:us-east-1:123456789012:function:stash-prod-api"
	for _, action := range []string{"lambda:CreateFunction", "lambda:UpdateFunctionCode", "lambda:UpdateFunctionConfiguration", "lambda:AddPermission", "lambda:GetFunction", "lambda:DeleteFunction"} {
		if !policy.Allows(Request{Action: action, Resource: sema}) {
			t.Errorf("%s is denied on a sema- function", action)
		}
		if policy.Allows(Request{Action: action, Resource: stash}) {
			t.Errorf("%s is allowed on another app's function", action)
		}
	}
	if !policy.Allows(Request{Action: "lambda:GetFunction", Resource: sema + ":1"}) {
		t.Error("cannot read a sema- function's version")
	}
	mapping := "arn:aws:lambda:us-east-1:123456789012:event-source-mapping:0b1c2d3e"
	for _, action := range []string{"lambda:CreateEventSourceMapping", "lambda:UpdateEventSourceMapping", "lambda:DeleteEventSourceMapping"} {
		if !policy.Allows(Request{Action: action, Resource: mapping, Context: map[string]string{"lambda:FunctionArn": sema}}) {
			t.Errorf("%s is denied for a sema- function", action)
		}
		if policy.Allows(Request{Action: action, Resource: mapping, Context: map[string]string{"lambda:FunctionArn": stash}}) {
			t.Errorf("%s is allowed for another app's function", action)
		}
	}
	for _, action := range []string{"lambda:GetEventSourceMapping", "lambda:ListEventSourceMappings", "lambda:ListTags"} {
		if !policy.Allows(Request{Action: action, Resource: mapping}) {
			t.Errorf("cannot %s on refresh", action)
		}
	}
}
