package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

func TestArchiveFiltersAcrossPages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query map[string]string
		want  []string
	}{
		{"tag", map[string]string{"tag": " TeCh "}, []string{"tech"}},
		{"feed", map[string]string{"feed": "tech"}, []string{"tech"}},
		{"untagged", map[string]string{"tag": "__untagged"}, []string{"plain"}},
		{"missing", map[string]string{"tag": "missing"}, []string{}},
		{"all", map[string]string{}, []string{"muted", "plain", "tech", "deleted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []string{"muted", "plain", "tech", "deleted"}
			calls := 0
			db := &apiDynamo{batchGet: func(*dynamodb.BatchGetItemInput) (*dynamodb.BatchGetItemOutput, error) {
				return &dynamodb.BatchGetItemOutput{}, nil
			}, query: func(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				index := 0
				if key, ok := input.ExclusiveStartKey["SK"].(*types.AttributeValueMemberS); ok {
					for i, id := range source {
						if key.Value == "A#"+id {
							index = i + 1
						}
					}
				}
				calls++
				id := source[index]
				row, err := attributevalue.MarshalMap(domain.Item{PK: domain.UserPK("user"), SK: "A#" + id, ItemID: id, FeedID: id})
				if err != nil {
					t.Fatal(err)
				}
				out := &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{row}}
				if index+1 < len(source) {
					out.LastEvaluatedKey = map[string]types.AttributeValue{"PK": row["PK"], "SK": row["SK"]}
				}
				return out, nil
			}}
			s := &server{store: store.New(db, nil, "table", "", ""), feedCache: map[string]cachedFeedList{"user": {loaded: time.Now(), feeds: []domain.Feed{
				{FeedID: "muted", Muted: true, Tags: []string{"tech"}}, {FeedID: "plain"}, {FeedID: "tech", Tags: []string{"tech"}},
			}}}}
			tc.query["limit"] = "1"
			got := []string{}
			for {
				response := s.getArchive(context.Background(), "user", tc.query)
				if response.StatusCode != http.StatusOK {
					t.Fatalf("response = %d %s", response.StatusCode, response.Body)
				}
				var body struct {
					Items []domain.Item `json:"items"`
					Next  string        `json:"next_cursor"`
				}
				if err := json.Unmarshal([]byte(response.Body), &body); err != nil {
					t.Fatal(err)
				}
				for _, item := range body.Items {
					if !item.Archived || !item.Hearted {
						t.Fatal("archive flags missing")
					}
					got = append(got, item.ItemID)
				}
				if body.Next == "" {
					break
				}
				if len(body.Items) == 0 {
					t.Fatal("empty intermediate filtered page")
				}
				tc.query["cursor"] = body.Next
			}
			if calls != len(source) {
				t.Fatalf("queries = %d", calls)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("items = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("items = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
