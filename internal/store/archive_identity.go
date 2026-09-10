package store

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

func archiveIdentity(userID, archiveSK string, live domain.Item) domain.ItemIdentity {
	return domain.ItemIdentity{
		PK: domain.UserPK(userID), SK: domain.ItemIdentitySK(live.ItemID), ItemSK: archiveSK,
		LiveSK: live.SK, LiveTTL: live.TTL,
	}
}

// BackfillArchiveIdentity installs a permanent lookup without racing unheart or
// replacing a newer identity. Existing permanent identities are already done.
func (s *Store) BackfillArchiveIdentity(ctx context.Context, userID string, archive domain.Item) error {
	if archive.ItemID == "" || archive.PK != domain.UserPK(userID) || !strings.HasPrefix(archive.SK, "A#") {
		return errors.New("invalid archive identity")
	}
	identityKey := key(archive.PK, domain.ItemIdentitySK(archive.ItemID))
	response, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table), Key: identityKey, ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return err
	}
	var previous domain.ItemIdentity
	if err := attributevalue.UnmarshalMap(response.Item, &previous); err != nil {
		return err
	}
	_, hasTTL := response.Item["ttl"]
	if strings.HasPrefix(previous.ItemSK, "A#") && !hasTTL {
		return nil
	}
	identity := domain.ItemIdentity{PK: archive.PK, SK: domain.ItemIdentitySK(archive.ItemID), ItemSK: archive.SK}
	if strings.HasPrefix(previous.ItemSK, "I#") {
		identity.LiveSK, identity.LiveTTL = previous.ItemSK, previous.TTL
	}
	encoded, err := attributevalue.MarshalMap(identity)
	if err != nil {
		return err
	}
	condition := "attribute_not_exists(SK)"
	values := map[string]types.AttributeValue{}
	names := map[string]string{}
	if len(response.Item) > 0 {
		condition = "attribute_exists(SK)"
		for _, attr := range []string{"item_sk", "ttl"} {
			names["#"+attr] = attr
			if value, ok := response.Item[attr]; ok {
				condition += " AND #" + attr + " = :" + attr
				values[":"+attr] = value
			} else {
				condition += " AND attribute_not_exists(#" + attr + ")"
			}
		}
	}
	if len(values) == 0 {
		values = nil
	}
	if len(names) == 0 {
		names = nil
	}
	err = s.transact(ctx, []types.TransactWriteItem{
		{ConditionCheck: &types.ConditionCheck{
			TableName: aws.String(s.table), Key: key(archive.PK, archive.SK),
			ConditionExpression:       aws.String("item_id = :id"),
			ExpressionAttributeValues: map[string]types.AttributeValue{":id": &types.AttributeValueMemberS{Value: archive.ItemID}},
		}},
		{Put: &types.Put{
			TableName: aws.String(s.table), Item: encoded, ConditionExpression: aws.String(condition),
			ExpressionAttributeNames: names, ExpressionAttributeValues: values,
		}},
	})
	if transactionConditionFailed(err) {
		return nil
	}
	return err
}
