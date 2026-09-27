package send

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type fakeSQS struct{ input *sqs.SendMessageInput }

func (f *fakeSQS) SendMessage(_ context.Context, input *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	f.input = input
	return &sqs.SendMessageOutput{}, nil
}

func TestSQSQueueDelaysTheRetryAndRoundTripsTheMessage(t *testing.T) {
	client := &fakeSQS{}
	message := Message{User: "user", DeliveryID: "delivery-1", Event: EventItemSend, URL: "https://receiver.example/hook", Body: []byte(`{"title":"<b>&</b>"}`), Attempts: 2}

	if err := (&SQSQueue{Client: client, URL: "https://sqs.example/deliveries"}).EnqueueRetry(context.Background(), message, 2*time.Minute); err != nil {
		t.Fatal(err)
	}

	if aws.ToString(client.input.QueueUrl) != "https://sqs.example/deliveries" || client.input.DelaySeconds != 120 {
		t.Fatalf("input = %+v", client.input)
	}
	var decoded Message
	if err := json.Unmarshal([]byte(aws.ToString(client.input.MessageBody)), &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded.Body) != string(message.Body) || decoded.Attempts != 2 || decoded.DeliveryID != "delivery-1" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestNewIDIsAVersion4UUID(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first, second := NewID(), NewID()
	if !pattern.MatchString(first) || first == second {
		t.Fatalf("ids = %s, %s", first, second)
	}
}
