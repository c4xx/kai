package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// FileRead reads a file and returns its contents (truncated to 32KB).
// Classified as READ_ONLY.
type FileRead struct{}

type fileReadParams struct {
	Path string `json:"path"`
}

func (FileRead) Name() string        { return "file_read" }
func (FileRead) Description() string { return "Read the contents of a file." }
func (FileRead) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file.",
			},
		},
		"required": []string{"path"},
	}
}

const maxFileReadBytes = 32 * 1024

func (FileRead) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var p fileReadParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("file_read: invalid params: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("file_read: path is required")
	}

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}
	if len(data) > maxFileReadBytes {
		return string(data[:maxFileReadBytes]) + "\n[... truncated]", nil
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// FileWrite writes content to a file (creates or overwrites).
// Classified as STATE_CHANGE.
type FileWrite struct{}

type fileWriteParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (FileWrite) Name() string        { return "file_write" }
func (FileWrite) Description() string { return "Write content to a file (creates or overwrites)." }
func (FileWrite) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to write.",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (FileWrite) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var p fileWriteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("file_write: invalid params: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("file_write: path is required")
	}
	if err := os.WriteFile(p.Path, []byte(p.Content), 0644); err != nil {
		return "", fmt.Errorf("file_write: %w", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
}
