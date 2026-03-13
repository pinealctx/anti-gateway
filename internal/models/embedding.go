package models

import "encoding/json"

// ========================
// Embeddings types
// ========================

// EmbeddingRequest is the standard OpenAI /v1/embeddings request.
// Ref: https://platform.openai.com/docs/api-reference/embeddings/create
type EmbeddingRequest struct {
	Model string          `json:"model"`
	Input json.RawMessage `json:"input"` // string | []string | []int | [][]int

	// Extras captures encoding_format, dimensions, user, and any future fields.
	Extras map[string]json.RawMessage `json:"-"`
}

var embeddingRequestKnownFields = map[string]bool{
	"model": true, "input": true,
}

func (r *EmbeddingRequest) UnmarshalJSON(data []byte) error {
	type alias EmbeddingRequest
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	r.Extras = captureExtras(data, embeddingRequestKnownFields)
	return nil
}

func (r EmbeddingRequest) MarshalJSON() ([]byte, error) {
	type alias EmbeddingRequest
	base, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	return mergeExtras(base, r.Extras)
}

// EmbeddingResponse is the standard OpenAI /v1/embeddings response.
type EmbeddingResponse struct {
	Object string          `json:"object"` // "list"
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  EmbeddingUsage  `json:"usage"`
}

// EmbeddingData holds one embedding vector.
type EmbeddingData struct {
	Object    string    `json:"object"` // "embedding"
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// EmbeddingUsage tracks token usage for embedding requests.
type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
