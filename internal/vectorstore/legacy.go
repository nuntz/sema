package vectorstore

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
)

type LegacyPurgeReport struct {
	Scanned int
	Legacy  int
	Deleted int
}

// PurgeLegacy is a one-time migration cleanup, separate from scheduled expiry.
// Only records without user_id metadata are eligible. Call after backfilling
// both indexes and switching all readers and writers to namespaced keys.
func (s *S3) PurgeLegacy(ctx context.Context, apply bool) (LegacyPurgeReport, error) {
	report := LegacyPurgeReport{}
	var token *string
	for {
		page, err := s.client.ListVectors(ctx, &s3vectors.ListVectorsInput{
			VectorBucketName: aws.String(s.bucket), IndexName: aws.String(s.index),
			NextToken: token, MaxResults: aws.Int32(500), ReturnMetadata: true,
		})
		if err != nil {
			return report, err
		}
		report.Scanned += len(page.Vectors)
		keys := []string{}
		for _, candidate := range page.Vectors {
			if candidate.Key == nil {
				continue
			}
			metadata := map[string]json.RawMessage{}
			if candidate.Metadata != nil {
				encoded, err := candidate.Metadata.MarshalSmithyDocument()
				if err != nil {
					return report, err
				}
				if err := json.Unmarshal(encoded, &metadata); err != nil {
					return report, err
				}
			}
			if _, hasOwner := metadata["user_id"]; hasOwner {
				continue
			}
			keys = append(keys, *candidate.Key)
		}
		report.Legacy += len(keys)
		if apply && len(keys) > 0 {
			if _, err := s.client.DeleteVectors(ctx, &s3vectors.DeleteVectorsInput{
				VectorBucketName: aws.String(s.bucket), IndexName: aws.String(s.index), Keys: keys,
			}); err != nil {
				return report, err
			}
			report.Deleted += len(keys)
		}
		token = page.NextToken
		if token == nil || *token == "" {
			return report, nil
		}
	}
}
