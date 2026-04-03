package safety

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestGate(t *testing.T) *Gate {
	t.Helper()
	dir := t.TempDir()
	pendingDir := filepath.Join(dir, "pending")
	if err := os.MkdirAll(pendingDir, 0700); err != nil {
		t.Fatal(err)
	}
	return NewGate(dir, "confirm")
}

func TestClassify(t *testing.T) {
	cases := []struct {
		tool string
		want BlastRadius
	}{
		{"file_read", ReadOnly},
		{"github_summary", ReadOnly},
		{"memory_store", IdempotentWrite},
		{"bash_exec", StateChange},
		{"file_write", StateChange},
		{"unknown_tool", StateChange},
	}
	for _, c := range cases {
		got := Classify(c.tool)
		if got != c.want {
			t.Errorf("Classify(%s) = %s, want %s", c.tool, got, c.want)
		}
	}
}

func TestNeedsConfirmation(t *testing.T) {
	confirmGate := NewGate(t.TempDir(), "confirm")
	autoGate := NewGate(t.TempDir(), "auto")

	if !confirmGate.NeedsConfirmation(StateChange) {
		t.Error("confirm gate should require confirmation for STATE_CHANGE")
	}
	if autoGate.NeedsConfirmation(StateChange) {
		t.Error("auto gate should not require confirmation for STATE_CHANGE")
	}
	if !confirmGate.NeedsConfirmation(Destructive) {
		t.Error("confirm gate should require confirmation for DESTRUCTIVE")
	}
	if !autoGate.NeedsConfirmation(Destructive) {
		t.Error("auto gate should still require confirmation for DESTRUCTIVE")
	}
	if confirmGate.NeedsConfirmation(ReadOnly) {
		t.Error("confirm gate should not require confirmation for READ_ONLY")
	}
}

func TestWriteAndConfirm(t *testing.T) {
	g := newTestGate(t)

	a := &PendingAction{
		ID:          "test-action-1",
		RunID:       "run-1",
		Tool:        "bash_exec",
		Params:      `{"cmd":"ls"}`,
		BlastRadius: StateChange,
		CreatedAt:   time.Now(),
	}
	if err := g.WritePending(a); err != nil {
		t.Fatal(err)
	}

	// Confirm immediately (not stale)
	if err := g.Confirm(a.ID, false); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// File should be gone
	if _, err := os.Stat(g.pendingPath(a.ID)); !os.IsNotExist(err) {
		t.Error("expected pending file to be removed after confirm")
	}
}

func TestStaleConfirmRequiresForce(t *testing.T) {
	g := newTestGate(t)

	a := &PendingAction{
		ID:          "stale-action",
		RunID:       "run-1",
		Tool:        "bash_exec",
		Params:      `{}`,
		BlastRadius: StateChange,
		CreatedAt:   time.Now().Add(-3 * time.Hour), // 3 hours ago
	}
	if err := g.WritePending(a); err != nil {
		t.Fatal(err)
	}

	// Without --force: should fail
	if err := g.Confirm(a.ID, false); err == nil {
		t.Error("expected error for stale action without --force")
	}

	// With --force: should succeed
	if err := g.Confirm(a.ID, true); err != nil {
		t.Fatalf("Confirm with --force: %v", err)
	}
}

func TestRejectSentinel(t *testing.T) {
	g := newTestGate(t)

	a := &PendingAction{
		ID:          "reject-action",
		RunID:       "run-1",
		Tool:        "bash_exec",
		Params:      `{}`,
		BlastRadius: Destructive,
		CreatedAt:   time.Now(),
	}
	if err := g.WritePending(a); err != nil {
		t.Fatal(err)
	}

	if err := g.Reject(a.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// Reject sentinel should exist
	if _, err := os.Stat(g.rejectPath(a.ID)); err != nil {
		t.Error("expected reject sentinel file to exist")
	}
}

func TestWaitForConfirm(t *testing.T) {
	g := newTestGate(t)

	a := &PendingAction{
		ID:          "wait-action",
		RunID:       "run-1",
		Tool:        "bash_exec",
		Params:      `{}`,
		BlastRadius: StateChange,
		CreatedAt:   time.Now(),
	}
	if err := g.WritePending(a); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	result := make(chan bool, 1)
	go func() {
		result <- g.WaitForConfirm(ctx, a.ID)
	}()

	// Remove the pending file after a short delay (simulates user confirming)
	time.Sleep(100 * time.Millisecond)
	os.Remove(g.pendingPath(a.ID))

	select {
	case confirmed := <-result:
		if !confirmed {
			t.Error("expected confirmed=true")
		}
	case <-time.After(3 * time.Second):
		t.Error("WaitForConfirm timed out")
	}
}

func TestWaitForConfirmCancelledContext(t *testing.T) {
	g := newTestGate(t)

	a := &PendingAction{
		ID:          "cancel-action",
		RunID:       "run-1",
		Tool:        "bash_exec",
		Params:      `{}`,
		BlastRadius: StateChange,
		CreatedAt:   time.Now(),
	}
	if err := g.WritePending(a); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() {
		result <- g.WaitForConfirm(ctx, a.ID)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case confirmed := <-result:
		if confirmed {
			t.Error("expected confirmed=false after context cancel")
		}
	case <-time.After(3 * time.Second):
		t.Error("WaitForConfirm did not return after context cancel")
	}
}

func TestListPending(t *testing.T) {
	g := newTestGate(t)

	for _, id := range []string{"a1", "a2", "a3"} {
		a := &PendingAction{
			ID: id, RunID: "run-1", Tool: "bash_exec",
			Params: `{}`, BlastRadius: StateChange, CreatedAt: time.Now(),
		}
		if err := g.WritePending(a); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := g.ListPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 pending, got %d", len(ids))
	}
}

func TestConfirmAlreadyResolved(t *testing.T) {
	g := newTestGate(t)
	// Confirming a non-existent action should not error
	if err := g.Confirm("nonexistent-id", false); err != nil {
		t.Errorf("expected no error for already-resolved action, got: %v", err)
	}
}
