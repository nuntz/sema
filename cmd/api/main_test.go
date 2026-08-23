package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

type apiDynamo struct {
	*dynamodb.Client
	batchGet func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error)
	getItem  func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
}

func (f *apiDynamo) BatchGetItem(_ context.Context, input *dynamodb.BatchGetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error) {
	return f.batchGet(input)
}

func (f *apiDynamo) GetItem(_ context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return f.getItem(input)
}

func TestParseIncludeRead(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
		err   bool
	}{
		{name: "default", value: "", want: false},
		{name: "unread only", value: "false", want: false},
		{name: "all items", value: "true", want: true},
		{name: "invalid", value: "yes", err: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseIncludeRead(test.value)
			if (err != nil) != test.err || got != test.want {
				t.Fatalf("parseIncludeRead(%q) = %v, %v", test.value, got, err)
			}
		})
	}
}

func TestPrepareItemsLoadsOnlyPageSignals(t *testing.T) {
	db := &apiDynamo{batchGet: func(input *dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
		request := input.RequestItems["table"]
		if len(request.Keys) != 2 {
			t.Fatalf("signal keys = %#v", request.Keys)
		}
		return &dynamodb.BatchGetItemOutput{Responses: map[string][]map[string]types.AttributeValue{"table": {
			{"SK": &types.AttributeValueMemberS{Value: "S#first"}, "value": &types.AttributeValueMemberN{Value: "1"}},
		}}}, nil
	}}
	server := &server{store: store.New(db, nil, "table", "", "/content")}
	items := []domain.Item{{ItemID: "first", Vector: []byte{1}}, {ItemID: "second", Vector: []byte{2}}}
	if err := server.prepareItems(context.Background(), "user", items); err != nil {
		t.Fatal(err)
	}
	if items[0].Signal != 1 || items[1].Signal != 0 || items[0].Vector != nil || items[1].Vector != nil {
		t.Fatalf("prepared items = %#v", items)
	}
}

func TestGetMeUsesProfileSignalCount(t *testing.T) {
	profile, err := attributevalue.MarshalMap(domain.User{
		PK: "U#user", SK: "PROFILE", Email: "reader@example.com", OrderPref: domain.OrderChrono, SignalCount: 12, HeartCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	db := &apiDynamo{
		getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			return &dynamodb.GetItemOutput{Item: profile}, nil
		},
		batchGet: func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
			t.Fatal("getMe unexpectedly loaded signals")
			return nil, nil
		},
	}
	response := (&server{store: store.New(db, nil, "table", "", "")}).getMe(context.Background(), "user")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.StatusCode, response.Body)
	}
	var body struct {
		SignalCount int         `json:"signal_count"`
		HeartCount  int         `json:"heart_count"`
		Profile     domain.User `json:"profile"`
	}
	if err := json.Unmarshal([]byte(response.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.SignalCount != 12 || body.Profile.SignalCount != 12 || body.HeartCount != 3 {
		t.Fatalf("profile response = %#v", body)
	}
}
