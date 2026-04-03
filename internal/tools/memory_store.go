package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c4xx/kai/internal/memory"
)

// MemoryStore stores a key-value preference in the database.
// Classified as IDEMPOTENT_WRITE.
type MemoryStore struct {
	db *memory.DB
}

// NewMemoryStore creates a MemoryStore tool.
func NewMemoryStore(db *memory.DB) *MemoryStore {
	return &MemoryStore{db: db}
}

type memoryStoreParams struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (m *MemoryStore) Name() string        { return "memory_store" }
func (m *MemoryStore) Description() string { return "Store a key-value preference for future use." }
func (m *MemoryStore) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type":        "string",
				"description": "Preference key (e.g. 'review_focus', 'team_member_alice').",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Value to store.",
			},
		},
		"required": []string{"key", "value"},
	}
}

func (m *MemoryStore) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var p memoryStoreParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("memory_store: invalid params: %w", err)
	}
	if p.Key == "" {
		return "", fmt.Errorf("memory_store: key is required")
	}
	if err := m.db.SetPref(p.Key, p.Value); err != nil {
		return "", fmt.Errorf("memory_store: %w", err)
	}
	return fmt.Sprintf("stored: %s = %s", p.Key, p.Value), nil
}
