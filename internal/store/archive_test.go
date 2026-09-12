package store

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type archiveHTMLStore struct {
	archiveObjectStore
	html    string
	saved   string
	failPut bool
}

func (s *archiveHTMLStore) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(s.html)), ContentType: aws.String("text/html")}, nil
}
func (s *archiveHTMLStore) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if s.failPut {
		return nil, errors.New("unavailable")
	}
	body, err := io.ReadAll(input.Body)
	s.saved = string(body)
	s.objects[aws.ToString(input.Key)] = true
	return &s3.PutObjectOutput{}, err
}
func TestArchiveBodyCopiesInlineImagesAndThumbnailsBeforeWritingHTML(t *testing.T) {
	for _, base := range []string{"", "https://content.example"} {
		objects := &archiveHTMLStore{archiveObjectStore: archiveObjectStore{objects: map[string]bool{
			"media/user/item/body-0.webp": true, "media/user/item/embed-0.webp": true,
		}}, html: `<img src="` + base + `/media/user/item/body-0.webp" srcset="` + base + `/media/user/item/body-0.webp 1x"><img src="` + base + `/media/user/item/embed-0.webp"><img src="https://elsewhere/image.jpg">`}
		repository := New(nil, objects, "table", "bucket", base)
		copied, err := repository.archiveBody(context.Background(), "user", "item", "bodies/user/item.html", "archive/user/item/body.html")
		if err != nil || !copied {
			t.Fatalf("archive = %v, %v", copied, err)
		}
		if strings.Contains(objects.saved, "/media/") || !strings.Contains(objects.saved, base+"/archive/user/item/embed-0.webp") || !strings.Contains(objects.saved, "https://elsewhere/image.jpg") {
			t.Fatal(objects.saved)
		}
		if !objects.objects["archive/user/item/body-0.webp"] || !objects.objects["archive/user/item/embed-0.webp"] {
			t.Fatal(objects.objects)
		}
		objects.html = objects.saved
		repository.deleteArchiveContent(context.Background(), "user", "item", nil)
		if objects.objects["archive/user/item/body-0.webp"] || objects.objects["archive/user/item/embed-0.webp"] {
			t.Fatal("inline archive assets survived removal")
		}
	}
}
func TestArchiveBodyDoesNotCommitHTMLWhenAssetMissing(t *testing.T) {
	objects := &archiveHTMLStore{archiveObjectStore: archiveObjectStore{objects: map[string]bool{}}, html: `<img src="/media/user/item/body-0.webp">`}
	copied, err := New(nil, objects, "table", "bucket", "").archiveBody(context.Background(), "user", "item", "body", "archive/user/item/body.html")
	if err == nil || copied || objects.saved != "" {
		t.Fatalf("archive = %v, %v, %q", copied, err, objects.saved)
	}
	objects.objects["media/user/item/body-0.webp"] = true
	objects.failPut = true
	copied, err = New(nil, objects, "table", "bucket", "").archiveBody(context.Background(), "user", "item", "body", "archive/user/item/body.html")
	if err == nil || copied {
		t.Fatalf("write failure = %v, %v", copied, err)
	}
}

type missingArchiveBody struct{ archiveObjectStore }

type unreadableArchiveBody struct{ archiveObjectStore }

func (s *unreadableArchiveBody) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return nil, &smithy.GenericAPIError{Code: "AccessDenied"}
}

func TestArchiveCleanupPreservesManifestWhenReadFails(t *testing.T) {
	objects := &unreadableArchiveBody{archiveObjectStore{objects: map[string]bool{
		"archive/user/item/body.html":   true,
		"archive/user/item/body-0.webp": true,
		"archive/user/item/lead.webp":   true,
	}}}
	New(nil, objects, "table", "bucket", "").deleteArchiveContent(context.Background(), "user", "item", nil)
	if len(objects.objects) != 3 {
		t.Fatalf("cleanup destroyed recovery data after failed read: %v", objects.objects)
	}
}

func TestArchiveCleanupDeletesMediaWhenBodyIsMissing(t *testing.T) {
	objects := &missingArchiveBody{archiveObjectStore{objects: map[string]bool{
		"archive/user/item/lead.webp": true,
	}}}
	New(nil, objects, "table", "bucket", "").deleteArchiveContent(context.Background(), "user", "item", nil)
	if len(objects.objects) != 0 {
		t.Fatalf("media survived cleanup: %v", objects.objects)
	}
}

func (s *missingArchiveBody) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return nil, &smithy.GenericAPIError{Code: "NoSuchKey"}
}
func TestArchiveMissingBodyRetainsMetadataOnlyBehavior(t *testing.T) {
	objects := &missingArchiveBody{archiveObjectStore{objects: map[string]bool{}}}
	copied, err := New(nil, objects, "table", "bucket", "").archiveBody(context.Background(), "user", "item", "body", "archive")
	if copied || err != nil {
		t.Fatalf("%v %v", copied, err)
	}
}

func TestArchiveItemMissingIdentityDoesNotQuery(t *testing.T) {
	for _, sk := range []string{"", "I#missing"} {
		queries := 0
		db := &fakeDynamoDB{
			getItem: func(*dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				row := map[string]types.AttributeValue{}
				if sk != "" {
					row["item_sk"] = &types.AttributeValueMemberS{Value: sk}
				}
				return &dynamodb.GetItemOutput{Item: row}, nil
			},
			query: func(*dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				queries++
				return &dynamodb.QueryOutput{}, nil
			},
		}
		_, err := New(db, nil, "table", "", "").ArchiveItem(context.Background(), "user", "missing")
		if !errors.Is(err, ErrNotFound) || queries != 0 {
			t.Fatalf("error = %v, queries = %d", err, queries)
		}
	}
}
