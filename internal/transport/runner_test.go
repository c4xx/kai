package transport

import (
	"context"
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// mockTool implements Tool for testing.
type mockTool struct {
	name   string
	output string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return "mock tool" }
func (m *mockTool) InputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return m.output, nil
}

func TestNewBetaRunner(t *testing.T) {
	// Just verify construction doesn't panic.
	runner := NewBetaRunner("sk-ant-test", anthropic.ModelClaude3_5HaikuLatest, 1024)
	if runner == nil {
		t.Fatal("expected non-nil runner")
	}
}

func TestBuildBetaTools(t *testing.T) {
	tools := []Tool{
		&mockTool{name: "file_read"},
		&mockTool{name: "bash_exec"},
	}
	params := buildBetaTools(tools)
	if len(params) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(params))
	}
	if params[0].OfTool == nil {
		t.Error("expected OfTool to be set for first tool")
	}
	if params[0].OfTool.Name != "file_read" {
		t.Errorf("expected file_read, got %s", params[0].OfTool.Name)
	}
}
