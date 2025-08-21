package vectorstore

import "os"

// NewFromEnv creates a VectorStore based on env configuration.
// MYCODER_VECTOR_PROVIDER: "noop"(default) | "pgvector"
// PG DSN env: MYCODER_PGVECTOR_DSN
func NewFromEnv() VectorStore {
	switch os.Getenv("MYCODER_VECTOR_PROVIDER") {
	case "pgvector":
		return PGVector{DSN: os.Getenv("MYCODER_PGVECTOR_DSN")}
	default:
		return Noop{}
	}
}
