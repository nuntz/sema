package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type policyMocks struct{ policies chan string }

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
