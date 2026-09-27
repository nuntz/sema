package send

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

// Message is one queued Send. Body is frozen when the user Sends, so every
// attempt carries identical bytes under the same delivery id.
type Message struct {
	User       string `json:"user"`
	DeliveryID string `json:"delivery_id"`
	Event      string `json:"event"`
	URL        string `json:"url"`
	Body       []byte `json:"body"`
	Attempts   int    `json:"attempts"`
}

type DestinationStore interface {
	Destination(ctx context.Context, userID string) (domain.Destination, error)
	RecordDelivery(ctx context.Context, userID string, at time.Time, status string) error
}

type Attempter interface {
	Attempt(ctx context.Context, delivery Delivery) Outcome
}

type RetryQueue interface {
	EnqueueRetry(ctx context.Context, message Message, delay time.Duration) error
}

// Deliverer runs one attempt of a Send and schedules the next one when the
// failure is retryable. The API uses it inline and the delivery worker for
// retries.
type Deliverer struct {
	Destinations DestinationStore
	Sender       Attempter
	Queue        RetryQueue
	Now          func() time.Time
}

type Result struct {
	Outcome   Outcome
	Queued    bool
	Cancelled bool
}

const cancelledStatus = "cancelled"

func (d *Deliverer) Deliver(ctx context.Context, message Message) (Result, error) {
	destination, err := d.Destinations.Destination(ctx, message.User)
	if errors.Is(err, store.ErrNotFound) {
		return Result{Cancelled: true}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("load destination: %w", err)
	}
	if !destination.Enabled || destination.URL != message.URL {
		d.record(ctx, message.User, cancelledStatus)
		return Result{Cancelled: true}, nil
	}
	outcome := d.Sender.Attempt(ctx, Delivery{ID: message.DeliveryID, Event: message.Event, URL: message.URL, Secret: destination.Secret, Body: message.Body})
	message.Attempts++
	result := Result{Outcome: outcome}
	if outcome.Kind == Retryable {
		if delay, ok := RetryDelay(message.Attempts); ok {
			if err := d.Queue.EnqueueRetry(ctx, message, delay); err != nil {
				d.record(ctx, message.User, outcome.Label())
				return result, fmt.Errorf("queue retry: %w", err)
			}
			result.Queued = true
		}
	}
	d.record(ctx, message.User, outcome.Label())
	return result, nil
}

// record is best effort: failing to store the status must not resend a
// delivery that already reached the Destination.
func (d *Deliverer) record(ctx context.Context, userID, status string) {
	if err := d.Destinations.RecordDelivery(ctx, userID, d.Now(), status); err != nil {
		slog.Warn("record delivery status failed", "user", userID, "status", status, "error", err)
	}
}
