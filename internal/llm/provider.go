package llm

import (
	"context"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatProvider provides chat completion APIs.
type ChatProvider interface {
	Chat(ctx context.Context, model string, messages []Message, stream bool, temperature float32) (ChatStream, error)
}

// Embedder provides embedding generation APIs.
type Embedder interface {
	Embeddings(ctx context.Context, model string, inputs []string) ([][]float32, error)
}

// ChatStream allows streaming tokens, or a single final message if non-streaming.
type ChatStream interface {
	Recv() (delta string, done bool, err error)
	Close() error
}
