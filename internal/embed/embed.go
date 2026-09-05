package embed

import "context"

type Embedder interface {
	Embed(context.Context, string) ([]float32, error)
}

type ImageEmbedder interface {
	EmbedImage(context.Context, []byte) ([]float32, error)
	EmbedText(context.Context, string) ([]float32, error)
}
