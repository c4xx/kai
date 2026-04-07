// Package safety classifies tool actions by blast radius and manages confirmation flow.
package safety

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BlastRadius classifies the potential impact of a tool action.
type BlastRadius string

const (
	ReadOnly        BlastRadius = "READ_ONLY"
	IdempotentWrite BlastRadius = "IDEMPOTENT_WRITE"
	StateChange     BlastRadius = "STATE_CHANGE"
	Destructive     BlastRadius = "DESTRUCTIVE"
)

// PendingAction is written to pending/<id>.json and read by the CLI confirm/reject commands.
type PendingAction struct {
	ID          string      `json:"id"`
	RunID       string      `json:"run_id"`
	Tool        string      `json:"tool"`
	Params      string      `json:"params"`
	BlastRadius BlastRadius `json:"blast_radius"`
	CreatedAt   time.Time   `json:"created_at"`
}

// Gate manages the pending confirmation directory and blast-radius classification.
type Gate struct {
	pendingDir  string
	stateChange string // "confirm" | "auto"
}

// NewGate creates a Gate using the pending/ subdirectory of dataDir.
func NewGate(dataDir, stateChangeTrust string) *Gate {
	return &Gate{
		pendingDir:  filepath.Join(dataDir, "pending"),
		stateChange: stateChangeTrust,
	}
}

// Classify returns the blast radius for a tool call.
// Tools not listed default to STATE_CHANGE (safe default).
func Classify(tool string) BlastRadius {
	switch tool {
	case "file_read", "github_summary", "memory_search":
		return ReadOnly
	case "memory_store":
		return IdempotentWrite
	case "bash_exec", "file_write":
		return StateChange
	default:
		return StateChange
	}
}

// pendingPath returns the path to the pending JSON file for an action.
func (g *Gate) pendingPath(id string) string {
	return filepath.Join(g.pendingDir, id+".json")
}

// rejectPath returns the path to the reject sentinel file.
func (g *Gate) rejectPath(id string) string {
	return filepath.Join(g.pendingDir, id+".rejected")
}

// WritePending writes the pending action JSON to disk.
func (g *Gate) WritePending(a *PendingAction) error {
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return os.WriteFile(g.pendingPath(a.ID), data, 0600)
}

// RemovePending removes the pending JSON file (called after confirmation or rejection).
func (g *Gate) RemovePending(id string) {
	os.Remove(g.pendingPath(id))
	os.Remove(g.rejectPath(id))
}

// NeedsConfirmation returns true if the blast radius requires user confirmation
// given the configured trust level.
func (g *Gate) NeedsConfirmation(br BlastRadius) bool {
	switch br {
	case Destructive:
		return true
	case StateChange:
		return g.stateChange != "auto"
	default:
		return false
	}
}

// WaitForConfirm blocks until the user confirms (pending file removed) or rejects
// (rejected sentinel appears), or ctx is cancelled (daemon shutdown).
// Returns true if confirmed, false if rejected or context cancelled.
func (g *Gate) WaitForConfirm(ctx context.Context, id string) bool {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if _, err := os.Stat(g.pendingPath(id)); os.IsNotExist(err) {
				// File removed = confirmed
				return true
			}
			if _, err := os.Stat(g.rejectPath(id)); err == nil {
				// Rejected sentinel found
				g.RemovePending(id)
				return false
			}
		}
	}
}

// ListPending returns all pending action IDs (filenames without extension).
func (g *Gate) ListPending() ([]string, error) {
	entries, err := os.ReadDir(g.pendingDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return ids, nil
}

// ReadPending reads the pending action JSON for a given ID.
func (g *Gate) ReadPending(id string) (*PendingAction, error) {
	data, err := os.ReadFile(g.pendingPath(id))
	if err != nil {
		return nil, err
	}
	var a PendingAction
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("corrupt pending file %s: %w", id, err)
	}
	return &a, nil
}

// Confirm confirms a pending action. If the action is older than 2 hours, force must be true.
// Returns an error if the action is stale and force is not set.
func (g *Gate) Confirm(id string, force bool) error {
	a, err := g.ReadPending(id)
	if os.IsNotExist(err) {
		fmt.Println("Action already resolved.")
		return nil
	}
	if err != nil {
		return err
	}

	age := time.Since(a.CreatedAt)
	if age > 2*time.Hour && !force {
		return fmt.Errorf(
			"action age: %s\ntool: %s | params: %s\nthis action is %s old. use --force to confirm",
			formatAge(age), a.Tool, a.Params, formatAge(age),
		)
	}

	// Confirmation = remove the pending file. WaitForConfirm() detects this.
	return os.Remove(g.pendingPath(id))
}

// Reject rejects a pending action by writing a sentinel file.
func (g *Gate) Reject(id string) error {
	if _, err := os.Stat(g.pendingPath(id)); os.IsNotExist(err) {
		fmt.Println("Action already resolved.")
		return nil
	}
	return os.WriteFile(g.rejectPath(id), []byte{}, 0600)
}

// ExpireStale removes pending actions older than maxAge and returns their IDs.
// Callers should log these IDs as auto-expired in the audit log.
func (g *Gate) ExpireStale(maxAge time.Duration) ([]string, error) {
	ids, err := g.ListPending()
	if err != nil {
		return nil, err
	}
	var expired []string
	for _, id := range ids {
		a, err := g.ReadPending(id)
		if err != nil {
			continue
		}
		if time.Since(a.CreatedAt) > maxAge {
			g.RemovePending(id)
			expired = append(expired, id)
		}
	}
	return expired, nil
}

func formatAge(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
