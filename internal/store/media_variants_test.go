package store

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

func TestUpdateMediaVariantsTouchesOnlyMediaManifestAndDimensions(t *testing.T) {
	var update *dynamodb.UpdateItemInput
	db := &fakeDynamoDB{updateItem: func(input *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
		update = input
		return &dynamodb.UpdateItemOutput{}, nil
	}}
	item := domain.Item{PK: "U#user", SK: "I#item", ItemID: "item"}
	variants := []domain.MediaVariant{{Key: "media/user/item/lead-384.jpg", Width: 384, Height: 256}, {Key: "media/user/item/lead.jpg", Width: 1280, Height: 853}}
	if err := New(db, nil, "table", "", "").UpdateMediaVariants(context.Background(), item, variants, 1280, 853); err != nil {
		t.Fatal(err)
	}
	if got := aws.ToString(update.UpdateExpression); got != "SET media_variants = :variants, media_w = :width, media_h = :height" {
		t.Fatalf("update expression = %q", got)
	}
	if aws.ToString(update.ConditionExpression) != "attribute_exists(PK) AND item_id = :item" {
		t.Fatalf("condition = %q", aws.ToString(update.ConditionExpression))
	}
	var decoded []domain.MediaVariant
	if err := attributevalue.Unmarshal(update.ExpressionAttributeValues[":variants"], &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[0] != variants[0] || update.ExpressionAttributeValues[":width"].(*types.AttributeValueMemberN).Value != "1280" || update.ExpressionAttributeValues[":height"].(*types.AttributeValueMemberN).Value != "853" {
		t.Fatalf("update values = %#v, variants %#v", update.ExpressionAttributeValues, decoded)
	}
}

func TestPublicItemResolvesResponsiveVariantURLs(t *testing.T) {
	item := domain.Item{
		MediaKey: "media/user/item/lead.jpg",
		MediaVariants: []domain.MediaVariant{
			{Key: "media/user/item/lead-384.jpg", Width: 384, Height: 256},
			{Key: "media/user/item/lead.jpg", Width: 1280, Height: 853},
		},
	}
	public := New(nil, nil, "", "", "https://content.example.com").PublicItem(item)
	if public.MediaKey != "https://content.example.com/media/user/item/lead.jpg" || public.MediaVariants[0].Key != "https://content.example.com/media/user/item/lead-384.jpg" || public.MediaVariants[1].Key != public.MediaKey {
		t.Fatalf("public item = %#v", public)
	}
	if item.MediaVariants[0].Key != "media/user/item/lead-384.jpg" {
		t.Fatalf("public conversion mutated stored manifest = %#v", item.MediaVariants)
	}
}
