package main

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/send"
)

type fakeDeliverer struct {
	messages []send.Message
	err      error
}

func (f *fakeDeliverer) Deliver(_ context.Context, message send.Message) (send.Result, error) {
	f.messages = append(f.messages, message)
	return send.Result{}, f.err
}

func TestHandleDeliversEachRecord(t *testing.T) {
	deliverer := &fakeDeliverer{}
	event := events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "m1", Body: `{"user":"user","delivery_id":"d1","event":"item.send","url":"https://receiver.example/hook","body":"eyJ2ZXJzaW9uIjoxfQ==","attempts":1}`},
	}}

	got, err := handle(context.Background(), deliverer, event)

	if err != nil || len(got.BatchItemFailures) != 0 {
		t.Fatalf("response = %+v, %v", got, err)
	}
	if len(deliverer.messages) != 1 || deliverer.messages[0].DeliveryID != "d1" || string(deliverer.messages[0].Body) != `{"version":1}` || deliverer.messages[0].Attempts != 1 {
		t.Fatalf("messages = %+v", deliverer.messages)
	}
}

func TestHandleReportsFailuresAndDropsMalformedRecords(t *testing.T) {
	deliverer := &fakeDeliverer{err: errors.New("dynamodb unavailable")}
	event := events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "bad", Body: `not json`},
		{MessageId: "retry", Body: `{"user":"user","delivery_id":"d2","event":"item.send","url":"https://receiver.example/hook","body":"e30=","attempts":2}`},
	}}

	got, err := handle(context.Background(), deliverer, event)

	if err != nil || len(got.BatchItemFailures) != 1 || got.BatchItemFailures[0].ItemIdentifier != "retry" {
		t.Fatalf("response = %+v, %v", got, err)
	}
}
