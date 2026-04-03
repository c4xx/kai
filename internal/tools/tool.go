// Package tools provides the 5 built-in kai tools:
// bash_exec, file_read, file_write, github_summary, memory_store.
package tools

import (
	"context"
	"encoding/json"
)

// Tool is the core interface for all kai tools.
// It mirrors transport.Tool but lives here to avoid import cycles.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	Execute(ctx context.Context, params json.RawMessage) (string, error)
}
