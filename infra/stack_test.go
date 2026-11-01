package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nuntz/sema/infra/deployaccess"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const mockAccount = "123456789012"

type registered struct {
	typeToken string
	name      string
	inputs    resource.PropertyMap
}

// stackMocks records everything the main program registers and invokes, and
// answers with just enough state for the program's outputs to resolve.
type stackMocks struct {
	mu        sync.Mutex
	resources []registered
	invokes   []string
}

func (m *stackMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.resources = append(m.resources, registered{typeToken: args.TypeToken, name: args.Name, inputs: args.Inputs})
	m.mu.Unlock()
	state := args.Inputs.Copy()
	set := func(key, value string) {
		if _, ok := state[resource.PropertyKey(key)]; !ok {
			state[resource.PropertyKey(key)] = resource.NewStringProperty(value)
		}
	}
	set("arn", "arn:aws:mock:us-east-1:"+mockAccount+":"+args.Name)
	set("name", args.Name)
	switch args.TypeToken {
	case "aws:s3/bucket:Bucket":
		set("bucket", args.Name)
		set("bucketRegionalDomainName", args.Name+".s3.us-east-1.amazonaws.com")
	case "aws:s3/vectorsIndex:VectorsIndex":
		set("indexArn", "arn:aws:s3vectors:us-east-1:"+mockAccount+":bucket/vectors/index/"+args.Name)
	case "aws:sqs/queue:Queue":
		set("url", "https://sqs.us-east-1.amazonaws.com/"+mockAccount+"/"+args.Name)
	case "aws:apigatewayv2/api:Api":
		set("apiEndpoint", "https://api.example")
		set("executionArn", "arn:aws:execute-api:us-east-1:"+mockAccount+":api")
	case "aws:cloudfront/distribution:Distribution":
		set("domainName", "d123.cloudfront.net")
		set("hostedZoneId", "Z2FDTNDATAQYW2")
	case "aws:acm/certificate:Certificate":
		state["domainValidationOptions"] = resource.NewArrayProperty([]resource.PropertyValue{resource.NewObjectProperty(resource.PropertyMap{
			"resourceRecordName":  resource.NewStringProperty("_validation.sema.example."),
			"resourceRecordValue": resource.NewStringProperty("_value.acm-validations.aws."),
		})})
	case "aws:route53/record:Record":
		set("fqdn", "_validation.sema.example")
	case "tls:index/privateKey:PrivateKey":
		set("publicKeyPem", "-----BEGIN PUBLIC KEY-----")
	}
	return args.Name + "-id", state, nil
}

func (m *stackMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	m.mu.Lock()
	m.invokes = append(m.invokes, args.Token)
	m.mu.Unlock()
	switch args.Token {
	case "aws:index/getCallerIdentity:getCallerIdentity":
		return resource.PropertyMap{
			"accountId": resource.NewStringProperty(mockAccount),
			"arn":       resource.NewStringProperty("arn:aws:sts::" + mockAccount + ":assumed-role/sema-github-deploy/ci"),
			"userId":    resource.NewStringProperty("AROAEXAMPLE:ci"),
			"id":        resource.NewStringProperty(mockAccount),
		}, nil
	case "aws:sns/getTopic:getTopic":
		return resource.PropertyMap{
			"arn":  resource.NewStringProperty("arn:aws:sns:us-east-1:" + mockAccount + ":NotifyMe"),
			"name": resource.NewStringProperty("NotifyMe"),
			"id":   resource.NewStringProperty("arn:aws:sns:us-east-1:" + mockAccount + ":NotifyMe"),
		}, nil
	}
	return nil, fmt.Errorf("unexpected provider call: %s", args.Token)
}

func (m *stackMocks) of(typeToken string) []registered {
	var matching []registered
	for _, entry := range m.resources {
		if entry.typeToken == typeToken {
			matching = append(matching, entry)
		}
	}
	return matching
}

// runStack runs the whole main program under mocks, with a custom domain and
// hosted zone so the certificate and DNS resources register too.
func runStack(t *testing.T) *stackMocks {
	t.Helper()
	t.Setenv("PULUMI_CONFIG", `{"sema:googleClientId":"client.apps.googleusercontent.com","sema:domain":"sema.example","sema:route53ZoneId":"Z0123456789ABC"}`)
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	mocks := &stackMocks{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return program(ctx, programAssets{build: func(string) error { return nil }, webDir: web})
	}, pulumi.WithMocks("sema", "test", mocks))
	if err != nil {
		t.Fatal(err)
	}
	return mocks
}

func TestEveryLambdaRoleIsFencedUnderTheSemaPath(t *testing.T) {
	mocks := runStack(t)
	roles := mocks.of("aws:iam/role:Role")
	if len(roles) != 7 {
		t.Fatalf("roles = %d, want one per Lambda (7)", len(roles))
	}
	for _, role := range roles {
		if got := role.inputs["path"]; !got.IsString() || got.StringValue() != deployaccess.RolePath {
			t.Errorf("%s path = %v, want %s", role.name, got, deployaccess.RolePath)
		}
		if got := role.inputs["permissionsBoundary"]; !got.IsString() || got.StringValue() != deployaccess.BoundaryARN(mockAccount) {
			t.Errorf("%s boundary = %v, want %s", role.name, got, deployaccess.BoundaryARN(mockAccount))
		}
		if _, named := role.inputs["name"]; named {
			t.Errorf("%s sets a name; roles keep generated names", role.name)
		}
	}
}

func TestEveryLambdaActionFitsInsideTheBoundary(t *testing.T) {
	mocks := runStack(t)
	boundary := deployaccess.BoundaryPolicy()
	policies := mocks.of("aws:iam/rolePolicy:RolePolicy")
	if len(policies) == 0 {
		t.Fatal("no role policies registered")
	}
	for _, entry := range policies {
		var policy struct {
			Statement []struct {
				Effect   string `json:"Effect"`
				Action   any    `json:"Action"`
				Resource any    `json:"Resource"`
			} `json:"Statement"`
		}
		if err := json.Unmarshal([]byte(entry.inputs["policy"].StringValue()), &policy); err != nil {
			t.Fatalf("%s: %v", entry.name, err)
		}
		for _, statement := range policy.Statement {
			if statement.Effect != "Allow" {
				continue
			}
			for _, action := range stringList(statement.Action) {
				for _, resource := range stringList(statement.Resource) {
					if !boundary.Allows(deployaccess.Request{Action: action, Resource: resource}) {
						t.Errorf("%s grants %s on %s, which the boundary denies", entry.name, action, resource)
					}
				}
			}
		}
	}
}

// iamNamespaces maps every AWS resource type and invoke the program uses to the
// IAM namespace that manages it. It lists each type rather than each Pulumi
// module because modules span services (cloudwatch holds events and logs), so
// any new type fails the test until someone decides its namespace.
var iamNamespaces = map[string]string{
	"acm/certificate": "acm", "acm/certificateValidation": "acm",
	"apigatewayv2/api": "apigateway", "apigatewayv2/integration": "apigateway", "apigatewayv2/route": "apigateway", "apigatewayv2/stage": "apigateway",
	"cloudfront/distribution": "cloudfront", "cloudfront/function": "cloudfront", "cloudfront/keyGroup": "cloudfront",
	"cloudfront/originAccessControl": "cloudfront", "cloudfront/publicKey": "cloudfront",
	"cloudwatch/dashboard": "cloudwatch", "cloudwatch/metricAlarm": "cloudwatch",
	"cloudwatch/eventRule": "events", "cloudwatch/eventTarget": "events",
	"cloudwatch/logGroup": "logs", "cloudwatch/queryDefinition": "logs",
	"dynamodb/table": "dynamodb",
	"iam/role":       "iam", "iam/rolePolicy": "iam",
	"lambda/function": "lambda", "lambda/eventSourceMapping": "lambda", "lambda/permission": "lambda",
	"route53/record": "route53",
	"s3/bucket":      "s3", "s3/bucketObject": "s3", "s3/bucketPolicy": "s3", "s3/bucketPublicAccessBlock": "s3", "s3/bucketLifecycleConfigurationV2": "s3",
	"s3/vectorsVectorBucket": "s3vectors", "s3/vectorsIndex": "s3vectors",
	"sns/getTopic": "sns",
	"sqs/queue":    "sqs",
	// GetCallerIdentity needs no permission.
	"index/getCallerIdentity": "",
}

func TestEveryAWSServiceTheStackUsesIsGrantedToTheDeployRole(t *testing.T) {
	mocks := runStack(t)
	granted := deployaccess.ServiceNamespaces()
	tokens := slices.Clone(mocks.invokes)
	for _, entry := range mocks.resources {
		tokens = append(tokens, entry.typeToken)
	}
	slices.Sort(tokens)
	for _, token := range slices.Compact(tokens) {
		parts := strings.Split(token, ":")
		if parts[0] != "aws" {
			continue // tls keys and Pulumi's own resources call no AWS API.
		}
		namespace, ok := iamNamespaces[parts[1]]
		if !ok {
			t.Errorf("%s: map it to an IAM namespace in iamNamespaces, then grant that namespace in deployaccess", token)
			continue
		}
		if namespace != "" && !slices.Contains(granted, namespace) {
			t.Errorf("%s needs %s, which the deploy role isn't granted", token, namespace)
		}
	}
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		list := make([]string, 0, len(typed))
		for _, entry := range typed {
			list = append(list, entry.(string))
		}
		return list
	}
	return nil
}
