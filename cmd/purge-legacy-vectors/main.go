// Command purge-legacy-vectors removes obsolete global vector records after
// the user-scoped migration. It is dry-run by default.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/nuntz/sema/internal/vectorstore"
)

func main() {
	apply := flag.Bool("apply", false, "delete records without user_id after verifying the namespaced backfill")
	image := flag.Bool("image", false, "inspect IMAGE_VECTOR_INDEX instead of VECTOR_INDEX")
	flag.Parse()
	ctx := context.Background()
	bucket, index := strings.TrimSpace(os.Getenv("VECTOR_BUCKET")), strings.TrimSpace(os.Getenv("VECTOR_INDEX"))
	if *image {
		index = strings.TrimSpace(os.Getenv("IMAGE_VECTOR_INDEX"))
	}
	if bucket == "" || index == "" {
		panic("VECTOR_BUCKET and the selected vector index are required")
	}
	awsConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		panic(err)
	}
	vectors := vectorstore.NewS3(s3vectors.NewFromConfig(awsConfig), bucket, index)
	report, err := vectors.PurgeLegacy(ctx, *apply)
	mode := "dry-run"
	if *apply {
		mode = "applied"
	}
	fmt.Printf("mode=%s index=%s scanned=%d legacy=%d deleted=%d\n", mode, index, report.Scanned, report.Legacy, report.Deleted)
	if err != nil {
		panic(err)
	}
}
