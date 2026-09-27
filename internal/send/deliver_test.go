package send

import (
	"context"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

type fakeDestinations struct {
	destination domain.Destination
	missing     bool
	recorded    []string
}

func (f *fakeDestinations) Destination(context.Context, string) (domain.Destination, error) {
	if f.missing {
		return domain.Destination{}, store.ErrNotFound
	}
	return f.destination, nil
}

func (f *fakeDestinations) RecordDelivery(_ context.Context, _ string, _ time.Time, status string) error {
	f.recorded = append(f.recorded, status)
	return nil
}

type fakeAttempter struct {
	outcome    Outcome
	deliveries []Delivery
}

func (f *fakeAttempter) Attempt(_ context.Context, delivery Delivery) Outcome {
	f.deliveries = append(f.deliveries, delivery)
	return f.outcome
}

type queuedRetry struct {
	message Message
	delay   time.Duration
}

type fakeRetryQueue struct{ retries []queuedRetry }

func (f *fakeRetryQueue) EnqueueRetry(_ context.Context, message Message, delay time.Duration) error {
	f.retries = append(f.retries, queuedRetry{message, delay})
	return nil
}

func deliveryFixture(outcome Outcome) (*Deliverer, *fakeDestinations, *fakeAttempter, *fakeRetryQueue) {
	destinations := &fakeDestinations{destination: domain.Destination{URL: "https://receiver.example/hook", Secret: "current-secret-0123456789abcdef01234", Enabled: true}}
	attempter := &fakeAttempter{outcome: outcome}
	queue := &fakeRetryQueue{}
	return &Deliverer{Destinations: destinations, Sender: attempter, Queue: queue, Now: func() time.Time { return attemptNow }}, destinations, attempter, queue
}

func queuedMessage(attempts int) Message {
	return Message{User: "user", DeliveryID: "delivery-1", Event: EventItemSend, URL: "https://receiver.example/hook", Body: []byte(`{"version":1}`), Attempts: attempts}
}

func TestDeliverRecordsADeliveredAttempt(t *testing.T) {
	deliverer, destinations, attempter, queue := deliveryFixture(Outcome{Kind: Delivered, Status: 202})

	result, err := deliverer.Deliver(context.Background(), queuedMessage(0))

	if err != nil || result.Outcome.Kind != Delivered || result.Queued {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if len(destinations.recorded) != 1 || destinations.recorded[0] != "202" || len(queue.retries) != 0 {
		t.Fatalf("recorded = %v, retries = %v", destinations.recorded, queue.retries)
	}
	got := attempter.deliveries[0]
	if got.ID != "delivery-1" || got.Secret != "current-secret-0123456789abcdef01234" || string(got.Body) != `{"version":1}` || got.Event != EventItemSend {
		t.Fatalf("delivery = %+v", got)
	}
}

func TestDeliverQueuesRetryableFailuresOnTheSchedule(t *testing.T) {
	for _, test := range []struct {
		attemptsBefore int
		wantDelay      time.Duration
	}{
		{attemptsBefore: 0, wantDelay: 5 * time.Second},
		{attemptsBefore: 1, wantDelay: 30 * time.Second},
		{attemptsBefore: 2, wantDelay: 2 * time.Minute},
	} {
		deliverer, destinations, _, queue := deliveryFixture(Outcome{Kind: Retryable, Status: 503})

		result, err := deliverer.Deliver(context.Background(), queuedMessage(test.attemptsBefore))

		if err != nil || !result.Queued {
			t.Fatalf("attempts %d: result = %+v, %v", test.attemptsBefore, result, err)
		}
		if len(queue.retries) != 1 || queue.retries[0].delay != test.wantDelay || queue.retries[0].message.Attempts != test.attemptsBefore+1 || queue.retries[0].message.DeliveryID != "delivery-1" {
			t.Fatalf("attempts %d: retries = %+v", test.attemptsBefore, queue.retries)
		}
		if len(destinations.recorded) != 1 || destinations.recorded[0] != "503" {
			t.Fatalf("recorded = %v", destinations.recorded)
		}
	}
}

func TestDeliverGivesUpAfterTheFourthAttempt(t *testing.T) {
	deliverer, destinations, _, queue := deliveryFixture(Outcome{Kind: Retryable, Reason: "timeout"})

	result, err := deliverer.Deliver(context.Background(), queuedMessage(3))

	if err != nil || result.Queued || len(queue.retries) != 0 {
		t.Fatalf("result = %+v, %v, retries %v", result, err, queue.retries)
	}
	if len(destinations.recorded) != 1 || destinations.recorded[0] != "timeout" {
		t.Fatalf("recorded = %v", destinations.recorded)
	}
}

func TestDeliverNeverRetriesPermanentFailures(t *testing.T) {
	deliverer, destinations, _, queue := deliveryFixture(Outcome{Kind: Permanent, Status: 401})

	result, _ := deliverer.Deliver(context.Background(), queuedMessage(0))

	if result.Queued || len(queue.retries) != 0 || destinations.recorded[0] != "401" {
		t.Fatalf("result = %+v, retries %v, recorded %v", result, queue.retries, destinations.recorded)
	}
}

func TestDeliverCancelsWhenTheDestinationChanged(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*fakeDestinations)
		record bool
	}{
		{name: "url changed", change: func(f *fakeDestinations) { f.destination.URL = "https://other.example/hook" }, record: true},
		{name: "disabled", change: func(f *fakeDestinations) { f.destination.Enabled = false }, record: true},
		{name: "removed", change: func(f *fakeDestinations) { f.missing = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			deliverer, destinations, attempter, queue := deliveryFixture(Outcome{Kind: Delivered, Status: 200})
			test.change(destinations)

			result, err := deliverer.Deliver(context.Background(), queuedMessage(1))

			if err != nil || !result.Cancelled || len(attempter.deliveries) != 0 || len(queue.retries) != 0 {
				t.Fatalf("result = %+v, %v, attempts %d", result, err, len(attempter.deliveries))
			}
			if test.record && (len(destinations.recorded) != 1 || destinations.recorded[0] != "cancelled") {
				t.Fatalf("recorded = %v", destinations.recorded)
			}
			if !test.record && len(destinations.recorded) != 0 {
				t.Fatalf("recorded = %v", destinations.recorded)
			}
		})
	}
}
