package send

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type sqsSender interface {
	SendMessage(ctx context.Context, input *sqs.SendMessageInput, options ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

// SQSQueue schedules retries on the deliveries queue using per-message delay.
type SQSQueue struct {
	Client sqsSender
	URL    string
}

func (q *SQSQueue) EnqueueRetry(ctx context.Context, message Message, delay time.Duration) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = q.Client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl: aws.String(q.URL), MessageBody: aws.String(string(encoded)), DelaySeconds: int32(delay / time.Second),
	})
	return err
}

// NewID returns a random RFC 4122 version 4 UUID.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
