package bedrock

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

const ModelID = "amazon.titan-embed-text-v2:0"

type runtimeAPI interface {
	InvokeModel(context.Context, *bedrockruntime.InvokeModelInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

type Client struct{ runtime runtimeAPI }

func New(runtime runtimeAPI) *Client { return &Client{runtime: runtime} }

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	payload, _ := json.Marshal(map[string]any{"inputText": text, "dimensions": 1024, "normalize": true})
	response, err := c.runtime.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId: aws.String(ModelID), Body: payload, ContentType: aws.String("application/json"), Accept: aws.String("application/json"),
	})
	if err != nil {
		return nil, fmt.Errorf("invoke Bedrock: %w", err)
	}
	var decoded struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(response.Body, &decoded); err != nil {
		return nil, fmt.Errorf("decode Bedrock response: %w", err)
	}
	if len(decoded.Embedding) != 1024 {
		return nil, fmt.Errorf("Bedrock returned %d dimensions, want 1024", len(decoded.Embedding))
	}
	return decoded.Embedding, nil
}
