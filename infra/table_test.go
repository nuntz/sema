package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"testing"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/dynamodb"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// dynamoTestTable is the DynamoDB Local table the root module's tests run on.
// infra is its own module and cannot import it, so the test reads its source.
const dynamoTestTable = "../internal/dynamotest/dynamotest.go"

type tableIndex struct {
	Name, Hash, Range, Projection string
	NonKeyAttributes              []string
}

// A projection that differs between the two tables keeps DynamoDB Local tests
// green while production returns the missing attributes empty.
func TestTableIndexesMatchDynamoDBLocalTable(t *testing.T) {
	want := pulumiIndexes(t, tableIndexes())
	got := dynamoTestIndexes(t)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s indexes = %+v\nwant infra/main.go's %+v", dynamoTestTable, got, want)
	}
}

func pulumiIndexes(t *testing.T, indexes dynamodb.TableGlobalSecondaryIndexArray) []tableIndex {
	t.Helper()
	text := func(input pulumi.StringInput) string {
		value, ok := input.(pulumi.String)
		if !ok {
			t.Fatalf("index field %#v is not a literal pulumi.String", input)
		}
		return string(value)
	}
	result := []tableIndex{}
	for _, input := range indexes {
		args := input.(*dynamodb.TableGlobalSecondaryIndexArgs)
		index := tableIndex{Name: text(args.Name), Projection: text(args.ProjectionType)}
		for _, key := range args.KeySchemas.(dynamodb.TableGlobalSecondaryIndexKeySchemaArray) {
			key := key.(*dynamodb.TableGlobalSecondaryIndexKeySchemaArgs)
			if text(key.KeyType) == "HASH" {
				index.Hash = text(key.AttributeName)
			} else {
				index.Range = text(key.AttributeName)
			}
		}
		if args.NonKeyAttributes != nil {
			for _, attribute := range args.NonKeyAttributes.(pulumi.StringArray) {
				index.NonKeyAttributes = append(index.NonKeyAttributes, text(attribute))
			}
		}
		result = append(result, index)
	}
	return result
}

func dynamoTestIndexes(t *testing.T) []tableIndex {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), dynamoTestTable, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]string{
		"KeyTypeHash": "HASH", "KeyTypeRange": "RANGE",
		"ProjectionTypeAll": "ALL", "ProjectionTypeKeysOnly": "KEYS_ONLY", "ProjectionTypeInclude": "INCLUDE",
	}
	// text reads aws.String("x"), "x" or types.Constant.
	var text func(ast.Expr) string
	text = func(expr ast.Expr) string {
		switch expr := expr.(type) {
		case *ast.BasicLit:
			value, err := strconv.Unquote(expr.Value)
			if err != nil {
				t.Fatal(err)
			}
			return value
		case *ast.CallExpr:
			if len(expr.Args) == 1 {
				return text(expr.Args[0])
			}
		case *ast.SelectorExpr:
			if value, ok := constants[expr.Sel.Name]; ok {
				return value
			}
		}
		t.Fatalf("cannot read %#v in %s", expr, dynamoTestTable)
		return ""
	}
	fields := func(literal ast.Expr) map[string]ast.Expr {
		if unary, ok := literal.(*ast.UnaryExpr); ok {
			literal = unary.X
		}
		result := map[string]ast.Expr{}
		for _, element := range literal.(*ast.CompositeLit).Elts {
			pair := element.(*ast.KeyValueExpr)
			result[pair.Key.(*ast.Ident).Name] = pair.Value
		}
		return result
	}
	var result []tableIndex
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		array, ok := literal.Type.(*ast.ArrayType)
		if !ok {
			return true
		}
		if element, ok := array.Elt.(*ast.SelectorExpr); !ok || element.Sel.Name != "GlobalSecondaryIndex" {
			return true
		}
		result = []tableIndex{}
		for _, element := range literal.Elts {
			index := fields(element)
			parsed := tableIndex{Name: text(index["IndexName"])}
			for _, key := range index["KeySchema"].(*ast.CompositeLit).Elts {
				key := fields(key)
				if text(key["KeyType"]) == "HASH" {
					parsed.Hash = text(key["AttributeName"])
				} else {
					parsed.Range = text(key["AttributeName"])
				}
			}
			projection := fields(index["Projection"])
			parsed.Projection = text(projection["ProjectionType"])
			if attributes, ok := projection["NonKeyAttributes"]; ok {
				for _, attribute := range attributes.(*ast.CompositeLit).Elts {
					parsed.NonKeyAttributes = append(parsed.NonKeyAttributes, text(attribute))
				}
			}
			result = append(result, parsed)
		}
		return false
	})
	if len(result) == 0 {
		t.Fatalf("found no []types.GlobalSecondaryIndex literal in %s", dynamoTestTable)
	}
	return result
}
