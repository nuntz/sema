package vectorstore

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/document"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/types"
)

type legacyPurgeClient struct {
	stubS3Vectors
	pages     [][]types.ListOutputVector
	listErr   error
	deleteErr error
}

func (s *legacyPurgeClient) ListVectors(_ context.Context, input *s3vectors.ListVectorsInput, _ ...func(*s3vectors.Options)) (*s3vectors.ListVectorsOutput, error) {
	if !input.ReturnMetadata {
		return nil, errors.New("metadata required")
	}
	page := 0
	if aws.ToString(input.NextToken) == "next" {
		page = 1
	}
	if page == 1 && s.listErr != nil {
		return nil, s.listErr
	}
	output := &s3vectors.ListVectorsOutput{}
	for _, record := range s.pages[page] {
		if !slices.Contains(s.deleted, aws.ToString(record.Key)) {
			output.Vectors = append(output.Vectors, record)
		}
	}
	if page == 0 {
		output.NextToken = aws.String("next")
	}
	return output, nil
}
func (s *legacyPurgeClient) DeleteVectors(ctx context.Context, input *s3vectors.DeleteVectorsInput, options ...func(*s3vectors.Options)) (*s3vectors.DeleteVectorsOutput, error) {
	if s.deleteErr != nil {
		return nil, s.deleteErr
	}
	return s.stubS3Vectors.DeleteVectors(ctx, input, options...)
}
func newLegacyPurgeClient() *legacyPurgeClient {
	record := func(key string, metadata map[string]any) types.ListOutputVector {
		return types.ListOutputVector{Key: aws.String(key), Metadata: document.NewLazyDocument(metadata)}
	}
	return &legacyPurgeClient{pages: [][]types.ListOutputVector{
		{
			record("old-archive", map[string]any{"kind": "archive"}),
			record(Key("one", "saved"), map[string]any{"kind": "archive", "user_id": "one"}),
		},
		{
			record("old-live", map[string]any{"kind": "live", "expires_ts": int64(9999999999)}),
			record(Key("two", "saved"), map[string]any{"kind": "live", "user_id": "two"}),
			record("empty-owner", map[string]any{"user_id": ""}),
		},
	}}
}

func TestPurgeLegacyIsDryRunSafePaginatedAndIdempotent(t *testing.T) {
	client := newLegacyPurgeClient()
	repository := NewS3(client, "bucket", "index")
	report, err := repository.PurgeLegacy(context.Background(), false)
	if err != nil || report != (LegacyPurgeReport{Scanned: 5, Legacy: 2}) || len(client.deleted) != 0 {
		t.Fatalf("dry run = %#v %v deleted=%v", report, err, client.deleted)
	}
	report, err = repository.PurgeLegacy(context.Background(), true)
	if err != nil || report != (LegacyPurgeReport{Scanned: 5, Legacy: 2, Deleted: 2}) || !reflect.DeepEqual(client.deleted, []string{"old-archive", "old-live"}) {
		t.Fatalf("apply = %#v %v deleted=%v", report, err, client.deleted)
	}
	report, err = repository.PurgeLegacy(context.Background(), true)
	if err != nil || report != (LegacyPurgeReport{Scanned: 3}) || len(client.deleted) != 2 {
		t.Fatalf("repeat = %#v %v deleted=%v", report, err, client.deleted)
	}
}

func TestPurgeLegacyReportsFailuresAndCanResume(t *testing.T) {
	for _, failure := range []string{"list", "delete"} {
		t.Run(failure, func(t *testing.T) {
			client := newLegacyPurgeClient()
			unavailable := errors.New("unavailable")
			if failure == "list" {
				client.listErr = unavailable
			} else {
				client.deleteErr = unavailable
			}
			repository := NewS3(client, "bucket", "index")
			report, err := repository.PurgeLegacy(context.Background(), true)
			if !errors.Is(err, unavailable) || report.Deleted != len(client.deleted) {
				t.Fatalf("failed report=%#v err=%v", report, err)
			}
			client.listErr, client.deleteErr = nil, nil
			if _, err := repository.PurgeLegacy(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(client.deleted, []string{"old-archive", "old-live"}) {
				t.Fatal(client.deleted)
			}
		})
	}
}
