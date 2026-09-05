package titanimage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

type fakeRuntime struct {
	input *bedrockruntime.InvokeModelInput
	body  []byte
	err   error
	calls int
}

func (f *fakeRuntime) InvokeModel(_ context.Context, input *bedrockruntime.InvokeModelInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	f.calls++
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	return &bedrockruntime.InvokeModelOutput{Body: f.body}, nil
}

func embeddingResponse(dimensions int) []byte {
	values := make([]float32, dimensions)
	body, _ := json.Marshal(map[string]any{"embedding": values})
	return body
}

func TestEmbedRequests(t *testing.T) {
	tests := []struct {
		name      string
		call      func(*Client) ([]float32, error)
		field     string
		wantValue string
	}{
		{name: "image", call: func(client *Client) ([]float32, error) {
			return client.EmbedImage(context.Background(), []byte{1, 2, 3})
		}, field: "inputImage", wantValue: base64.StdEncoding.EncodeToString([]byte{1, 2, 3})},
		{name: "text", call: func(client *Client) ([]float32, error) {
			return client.EmbedText(context.Background(), strings.Repeat("界", MaxTextRunes+20))
		}, field: "inputText", wantValue: strings.Repeat("界", MaxTextRunes)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := &fakeRuntime{body: embeddingResponse(Dimensions)}
			vector, err := test.call(NewWithModel(runtime, "custom-model"))
			if err != nil {
				t.Fatal(err)
			}
			if len(vector) != Dimensions || aws.ToString(runtime.input.ModelId) != "custom-model" || aws.ToString(runtime.input.ContentType) != "application/json" {
				t.Fatalf("vector/model/content-type = %d/%q/%q", len(vector), aws.ToString(runtime.input.ModelId), aws.ToString(runtime.input.ContentType))
			}
			var request map[string]any
			if err := json.Unmarshal(runtime.input.Body, &request); err != nil {
				t.Fatal(err)
			}
			if request[test.field] != test.wantValue {
				t.Fatalf("%s = %q, want %q", test.field, request[test.field], test.wantValue)
			}
			config, ok := request["embeddingConfig"].(map[string]any)
			if !ok || config["outputEmbeddingLength"] != float64(Dimensions) {
				t.Fatalf("embeddingConfig = %#v", request["embeddingConfig"])
			}
		})
	}
}

func TestEmbedValidationAndErrors(t *testing.T) {
	tests := []struct {
		name      string
		runtime   *fakeRuntime
		call      func(*Client) ([]float32, error)
		wantError string
		wantCalls int
	}{
		{name: "empty image", runtime: &fakeRuntime{}, call: func(client *Client) ([]float32, error) { return client.EmbedImage(context.Background(), nil) }, wantError: "empty", wantCalls: 0},
		{name: "oversize image", runtime: &fakeRuntime{}, call: func(client *Client) ([]float32, error) {
			return client.EmbedImage(context.Background(), make([]byte, MaxImageBytes+1))
		}, wantError: "maximum", wantCalls: 0},
		{name: "runtime", runtime: &fakeRuntime{err: errors.New("unavailable")}, call: func(client *Client) ([]float32, error) { return client.EmbedText(context.Background(), "fog") }, wantError: "invoke Bedrock", wantCalls: 1},
		{name: "response", runtime: &fakeRuntime{body: []byte("{")}, call: func(client *Client) ([]float32, error) { return client.EmbedText(context.Background(), "fog") }, wantError: "decode Bedrock response", wantCalls: 1},
		{name: "dimensions", runtime: &fakeRuntime{body: embeddingResponse(Dimensions - 1)}, call: func(client *Client) ([]float32, error) { return client.EmbedText(context.Background(), "fog") }, wantError: "1023 dimensions", wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.call(New(test.runtime))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
			if test.runtime.calls != test.wantCalls {
				t.Fatalf("runtime calls = %d, want %d", test.runtime.calls, test.wantCalls)
			}
		})
	}
}
