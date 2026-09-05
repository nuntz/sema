package titanimage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

const (
	ModelID       = "amazon.titan-embed-image-v1"
	Dimensions    = 1024
	MaxImageBytes = 5 << 20
	MaxTextRunes  = 400
)

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

func (c *Client) EmbedImage(ctx context.Context, jpeg []byte) ([]float32, error) {
	if len(jpeg) == 0 {
		return nil, errors.New("image payload is empty")
	}
	if len(jpeg) > MaxImageBytes {
		return nil, fmt.Errorf("image payload is %d bytes, maximum is %d", len(jpeg), MaxImageBytes)
	}
	return c.invoke(ctx, map[string]any{
		"inputImage":      base64.StdEncoding.EncodeToString(jpeg),
		"embeddingConfig": map[string]int{"outputEmbeddingLength": Dimensions},
	})
}

func (c *Client) EmbedText(ctx context.Context, value string) ([]float32, error) {
	if utf8.RuneCountInString(value) > MaxTextRunes {
		value = string([]rune(value)[:MaxTextRunes])
	}
	return c.invoke(ctx, map[string]any{
		"inputText":       value,
		"embeddingConfig": map[string]int{"outputEmbeddingLength": Dimensions},
	})
}

func (c *Client) invoke(ctx context.Context, request map[string]any) ([]float32, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode Bedrock request: %w", err)
	}
	response, err := c.runtime.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId: aws.String(c.modelID), Body: payload, ContentType: aws.String("application/json"), Accept: aws.String("application/json"),
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
	if len(decoded.Embedding) != Dimensions {
		return nil, fmt.Errorf("Bedrock returned %d dimensions, want %d", len(decoded.Embedding), Dimensions)
	}
	return decoded.Embedding, nil
}
