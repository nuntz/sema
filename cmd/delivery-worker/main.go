package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/nuntz/sema/internal/send"
	"github.com/nuntz/sema/internal/store"
)

type deliverer interface {
	Deliver(ctx context.Context, message send.Message) (send.Result, error)
}

// handle retries queued Sends. A malformed record can never succeed, so it is
// dropped rather than cycled into the dead-letter queue.
func handle(ctx context.Context, d deliverer, event events.SQSEvent) (events.SQSEventResponse, error) {
	response := events.SQSEventResponse{}
	for _, record := range event.Records {
		var message send.Message
		if err := json.Unmarshal([]byte(record.Body), &message); err != nil {
			slog.Error("drop malformed delivery", "message_id", record.MessageId, "error", err)
			continue
		}
		result, err := d.Deliver(ctx, message)
		if err != nil {
			slog.Error("delivery failed", "user", message.User, "delivery_id", message.DeliveryID, "attempts", message.Attempts, "error", err)
			response.BatchItemFailures = append(response.BatchItemFailures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
			continue
		}
		slog.Info("delivery attempted", "user", message.User, "delivery_id", message.DeliveryID, "attempt", message.Attempts+1, "outcome", result.Outcome.Kind, "status", result.Outcome.Label(), "queued", result.Queued, "cancelled", result.Cancelled)
	}
	return response, nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	repository, config, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	queueURL := strings.TrimSpace(os.Getenv("DELIVERIES_QUEUE_URL"))
	if queueURL == "" {
		panic("DELIVERIES_QUEUE_URL is required")
	}
	d := &send.Deliverer{
		Destinations: repository, Sender: send.NewSender(),
		Queue: &send.SQSQueue{Client: sqs.NewFromConfig(config), URL: queueURL}, Now: time.Now,
	}
	lambda.Start(func(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
		return handle(ctx, d, event)
	})
}
