package models

import "time"

type Project struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	RootPath string    `json:"rootPath"`
	Ignore   []string  `json:"ignore,omitempty"`
	Created  time.Time `json:"createdAt"`
}

type IndexMode string

const (
	IndexFull        IndexMode = "full"
	IndexIncremental IndexMode = "incremental"
)

type IndexJobStatus string

const (
	JobPending   IndexJobStatus = "pending"
	JobRunning   IndexJobStatus = "running"
	JobCompleted IndexJobStatus = "completed"
	JobFailed    IndexJobStatus = "failed"
)

type IndexJob struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"projectID"`
	Mode      IndexMode      `json:"mode"`
	Status    IndexJobStatus `json:"status"`
	StartedAt time.Time      `json:"startedAt"`
	EndedAt   *time.Time     `json:"endedAt,omitempty"`
	Stats     map[string]int `json:"stats,omitempty"`
}

type Document struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectID"`
	Path      string `json:"path"`
	Content   string `json:"-"`
}

type SearchResult struct {
	Path      string  `json:"path"`
	Score     float64 `json:"score"`
	Preview   string  `json:"preview,omitempty"`
	StartLine int     `json:"startLine,omitempty"`
	EndLine   int     `json:"endLine,omitempty"`
}

// Knowledge entities for curated, verified information.
type Knowledge struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"projectID"`
	SourceType string  `json:"sourceType"` // code|doc|web
	PathOrURL  string  `json:"pathOrURL"`
	Title      string  `json:"title,omitempty"`
	Text       string  `json:"text"`
	TrustScore float64 `json:"trustScore"`
	Pinned     bool    `json:"pinned"`
	CommitSHA  string  `json:"commitSHA,omitempty"`
	Files      string  `json:"files,omitempty"`
	Symbols    string  `json:"symbols,omitempty"`
	Tags       string  `json:"tags,omitempty"`
}

// Run/ExecutionLog models for recording executions (shell/fs/hooks/mcp)
type Run struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"projectID"`
	Type      string     `json:"type"`   // chat|edit|index|hooks|shell|fs|mcp
	Status    string     `json:"status"` // pending|running|completed|failed
	StartedAt time.Time  `json:"startedAt"`
	Finished  *time.Time `json:"finishedAt,omitempty"`
	Metrics   string     `json:"metrics,omitempty"`
	LogsRef   string     `json:"logsRef,omitempty"`
}

type ExecutionLog struct {
	ID         string     `json:"id"`
	RunID      string     `json:"runID"`
	Kind       string     `json:"kind"` // shell|fs|hook|mcp
	PayloadRef string     `json:"payloadRef,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	ExitCode   int        `json:"exitCode"`
}
