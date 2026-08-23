package bedrock

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

const ModelID = "amazon.nova-micro-v1:0"

type runtimeAPI interface {
	InvokeModel(context.Context, *bedrockruntime.InvokeModelInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

type Client struct {
	runtime runtimeAPI
	modelID string
}

func New(runtime runtimeAPI) *Client { return NewWithModel(runtime, ModelID) }

func NewWithModel(runtime runtimeAPI, modelID string) *Client {
	if modelID == "" {
		modelID = ModelID
	}
	return &Client{runtime: runtime, modelID: modelID}
}

func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"schemaVersion": "messages-v1",
		"messages": []any{map[string]any{
			"role": "user", "content": []any{map[string]string{"text": prompt}},
		}},
		"inferenceConfig": map[string]any{"maxTokens": 120, "temperature": 0},
	})
	response, err := c.runtime.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId: aws.String(c.modelID), Body: payload, ContentType: aws.String("application/json"), Accept: aws.String("application/json"),
	})
	if err != nil {
		return "", fmt.Errorf("invoke Bedrock summarizer: %w", err)
	}
	var decoded struct {
		Output struct {
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
	}
	if err := json.Unmarshal(response.Body, &decoded); err != nil {
		return "", fmt.Errorf("decode Bedrock summary response: %w", err)
	}
	if len(decoded.Output.Message.Content) == 0 {
		return "", fmt.Errorf("Bedrock summarizer returned no content")
	}
	return decoded.Output.Message.Content[0].Text, nil
}
