package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// BashExec executes a shell command and returns combined stdout+stderr.
// Classified as STATE_CHANGE by the safety gate.
type BashExec struct{}

type bashExecParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout_seconds"` // optional, default 30
}

func (BashExec) Name() string        { return "bash_exec" }
func (BashExec) Description() string { return "Execute a shell command and return its output." }
func (BashExec) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute.",
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds (default 30, max 300).",
			},
		},
		"required": []string{"command"},
	}
}

func (BashExec) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var p bashExecParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("bash_exec: invalid params: %w", err)
	}
	if p.Command == "" {
		return "", fmt.Errorf("bash_exec: command is required")
	}
	timeout := 30 * time.Second
	if p.Timeout > 0 && p.Timeout <= 300 {
		timeout = time.Duration(p.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", p.Command)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		// Return output even on error — stderr is often the useful part.
		output := strings.TrimRight(buf.String(), "\n")
		if output == "" {
			return "", fmt.Errorf("bash_exec: %w", err)
		}
		return output, fmt.Errorf("bash_exec exit %w", err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
