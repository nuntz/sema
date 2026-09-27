package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type policyMocks struct{ policies chan string }

func TestAPIConditionCheckPermissions(t *testing.T) {
	policies := make(chan string, 1)
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		output := func(value string) pulumi.StringOutput { return pulumi.String(value).ToStringOutput() }
		_, err := lambdaRole(ctx, "api", output("table"), output("arn:aws:s3:::content"), output("feeds"), output("items"), output("vectors"), output("images"))
		return err
	}, pulumi.WithMocks("sema", "test", policyMocks{policies: policies}))
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	select {
	case raw = <-policies:
	default:
		t.Fatal("API role policy was not created")
	}
	var policy struct {
		Statements []struct {
			Effect   string `json:"Effect"`
			Action   any    `json:"Action"`
			Resource any    `json:"Resource"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		t.Fatal(err)
	}
	contains := func(value any, wanted string) bool {
		if single, ok := value.(string); ok {
			return single == wanted
		}
		entries, ok := value.([]any)
		return ok && slices.Contains(entries, any(wanted))
	}
	for _, statement := range policy.Statements {
		if statement.Effect == "Allow" && contains(statement.Resource, "table") && contains(statement.Action, "dynamodb:ConditionCheckItem") {
			return
		}
	}
	t.Fatal("API table policy lacks dynamodb:ConditionCheckItem")
}

func (m policyMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if args.TypeToken == "aws:iam/rolePolicy:RolePolicy" {
		m.policies <- args.Inputs["policy"].StringValue()
	}
	return args.Name + "-id", args.Inputs, nil
}

func (policyMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return nil, fmt.Errorf("unexpected provider call: %s", args.Token)
}

func TestAPIArchiveCleanupPermissions(t *testing.T) {
	policies := make(chan string, 1)
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		output := func(value string) pulumi.StringOutput { return pulumi.String(value).ToStringOutput() }
		_, err := lambdaRole(ctx, "api", output("table"), output("arn:aws:s3:::content"), output("feeds"), output("items"), output("vectors"), output("images"))
		return err
	}, pulumi.WithMocks("sema", "test", policyMocks{policies: policies}))
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	select {
	case raw = <-policies:
	default:
		t.Fatal("API role policy was not created")
	}
	var policy struct {
		Statements []struct {
			Effect   string `json:"Effect"`
			Action   any    `json:"Action"`
			Resource any    `json:"Resource"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		t.Fatal(err)
	}
	contains := func(value any, wanted string) bool {
		if single, ok := value.(string); ok {
			return single == wanted
		}
		for _, entry := range value.([]any) {
			if entry == wanted {
				return true
			}
		}
		return false
	}
	listBucketAllowed := false
	for _, statement := range policy.Statements {
		if statement.Effect == "Allow" && contains(statement.Action, "s3:ListBucket") {
			if statement.Resource != "arn:aws:s3:::content" {
				t.Errorf("ListBucket resource = %#v, want content bucket ARN", statement.Resource)
			}
			listBucketAllowed = statement.Resource == "arn:aws:s3:::content"
		}
	}
	if !listBucketAllowed {
		t.Error("API archive cleanup needs s3:ListBucket so missing objects return 404")
	}
	for _, action := range []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"} {
		allowed := false
		for _, statement := range policy.Statements {
			if statement.Effect == "Allow" && contains(statement.Resource, "arn:aws:s3:::content/archive/*") && contains(statement.Action, action) {
				allowed = true
			}
		}
		if !allowed {
			t.Errorf("API archive cleanup lacks %s", action)
		}
	}
}

type policyStatement struct {
	Effect   string `json:"Effect"`
	Action   any    `json:"Action"`
	Resource any    `json:"Resource"`
}

func collectPolicies(t *testing.T, program func(ctx *pulumi.Context) error, count int) [][]policyStatement {
	t.Helper()
	policies := make(chan string, count+1)
	if err := pulumi.RunErr(program, pulumi.WithMocks("sema", "test", policyMocks{policies: policies})); err != nil {
		t.Fatal(err)
	}
	close(policies)
	var decoded [][]policyStatement
	for raw := range policies {
		var policy struct {
			Statements []policyStatement `json:"Statement"`
		}
		if err := json.Unmarshal([]byte(raw), &policy); err != nil {
			t.Fatal(err)
		}
		decoded = append(decoded, policy.Statements)
	}
	if len(decoded) != count {
		t.Fatalf("policies = %d, want %d", len(decoded), count)
	}
	return decoded
}

func policyActions(statements []policyStatement, resource string) []string {
	var actions []string
	for _, statement := range statements {
		resources, _ := statement.Resource.([]any)
		if single, ok := statement.Resource.(string); ok {
			resources = []any{single}
		}
		if statement.Effect != "Allow" || !slices.Contains(resources, any(resource)) {
			continue
		}
		switch action := statement.Action.(type) {
		case string:
			actions = append(actions, action)
		case []any:
			for _, entry := range action {
				actions = append(actions, entry.(string))
			}
		}
	}
	slices.Sort(actions)
	return actions
}

func TestDeliveryWorkerRoleOnlyReadsAndUpdatesTheTable(t *testing.T) {
	policies := collectPolicies(t, func(ctx *pulumi.Context) error {
		output := func(value string) pulumi.StringOutput { return pulumi.String(value).ToStringOutput() }
		_, err := lambdaRole(ctx, "delivery-worker", output("table"), output("arn:aws:s3:::content"), output("feeds"), output("items"), output("vectors"), output("images"))
		return err
	}, 1)

	if got := policyActions(policies[0], "table"); !slices.Equal(got, []string{"dynamodb:GetItem", "dynamodb:UpdateItem"}) {
		t.Fatalf("table actions = %v", got)
	}
	for _, resource := range []string{"arn:aws:s3:::content", "feeds", "items", "vectors", "images"} {
		if got := policyActions(policies[0], resource); len(got) != 0 {
			t.Fatalf("%s actions = %v", resource, got)
		}
	}
}

func TestDeliveryQueuePoliciesLetTheAPISendAndTheWorkerConsumeAndRequeue(t *testing.T) {
	policies := collectPolicies(t, func(ctx *pulumi.Context) error {
		role := func(name string) *iam.Role {
			created, _ := iam.NewRole(ctx, name, &iam.RoleArgs{AssumeRolePolicy: pulumi.String("{}")})
			return created
		}
		return deliveryQueuePolicies(ctx, role("api-role"), role("delivery-worker-role"), pulumi.String("deliveries").ToStringOutput())
	}, 2)

	var got [][]string
	for _, statements := range policies {
		got = append(got, policyActions(statements, "deliveries"))
	}
	slices.SortFunc(got, func(a, b []string) int { return len(a) - len(b) })
	if !slices.Equal(got[0], []string{"sqs:SendMessage"}) {
		t.Fatalf("api actions = %v", got[0])
	}
	if !slices.Equal(got[1], []string{"sqs:ChangeMessageVisibility", "sqs:DeleteMessage", "sqs:GetQueueAttributes", "sqs:ReceiveMessage", "sqs:SendMessage"}) {
		t.Fatalf("worker actions = %v", got[1])
	}
}

func TestAPICanWriteSendCopiesButNotReadThemBack(t *testing.T) {
	policies := collectPolicies(t, func(ctx *pulumi.Context) error {
		output := func(value string) pulumi.StringOutput { return pulumi.String(value).ToStringOutput() }
		_, err := lambdaRole(ctx, "api", output("table"), output("arn:aws:s3:::content"), output("feeds"), output("items"), output("vectors"), output("images"))
		return err
	}, 1)

	if got := policyActions(policies[0], "arn:aws:s3:::content/send/*"); !slices.Equal(got, []string{"s3:PutObject"}) {
		t.Fatalf("send/ actions = %v", got)
	}
}
