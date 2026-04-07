package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c4xx/kai/internal/config"
	"github.com/c4xx/kai/internal/memory"
	"github.com/c4xx/kai/internal/safety"
)

func newTestDB(t *testing.T) (*memory.DB, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pending"), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := memory.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, dir
}

func writePending(t *testing.T, dir, id string, createdAt time.Time) {
	t.Helper()
	a := safety.PendingAction{
		ID:          id,
		RunID:       "run-1",
		Tool:        "bash_exec",
		Params:      `{"command":"echo hi"}`,
		BlastRadius: safety.StateChange,
		CreatedAt:   createdAt,
	}
	data, _ := json.Marshal(a)
	path := filepath.Join(dir, "pending", id+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestExpireStale(t *testing.T) {
	db, dir := newTestDB(t)
	cfg := &config.Config{
		DataDir: dir,
		Trust:   config.TrustConfig{StateChange: "confirm"},
	}

	// Write one fresh and one 8-day-old pending action.
	freshID := "fresh-action-id"
	staleID := "stale-action-id"
	writePending(t, dir, freshID, time.Now().Add(-1*time.Hour))
	writePending(t, dir, staleID, time.Now().Add(-8*24*time.Hour))

	expireStale(cfg, db)

	// Fresh file must still exist.
	if _, err := os.Stat(filepath.Join(dir, "pending", freshID+".json")); err != nil {
		t.Errorf("fresh pending file should still exist: %v", err)
	}

	// Stale file must be removed.
	if _, err := os.Stat(filepath.Join(dir, "pending", staleID+".json")); !os.IsNotExist(err) {
		t.Error("stale pending file should have been removed")
	}
}

func TestExpireStale_NoPendingDir(t *testing.T) {
	db, dir := newTestDB(t)
	cfg := &config.Config{
		DataDir: dir,
		Trust:   config.TrustConfig{StateChange: "confirm"},
	}
	// Remove the pending dir entirely — should not panic.
	os.RemoveAll(filepath.Join(dir, "pending"))
	expireStale(cfg, db) // must not panic or error
}

func TestBuildFTSQuery_ViaClassifyCache(t *testing.T) {
	// Test that classifyRepo stores and retrieves a cached classification.
	db, dir := newTestDB(t)
	_ = dir

	// Pre-seed a cached "active" classification with a future expiry.
	expiry := time.Now().Add(time.Hour).Unix()
	_ = db.SetPref("repo_activity:alice/myapp", "active")
	_ = db.SetPref("repo_activity_expires:alice/myapp", formatInt(expiry))

	// classifyRepo should return the cached value without hitting the network.
	// We pass a nil client — if it hits the network it will panic.
	active, err := classifyRepo(context.Background(), db, nil, "alice/myapp")
	if err != nil {
		t.Fatalf("classifyRepo: %v", err)
	}
	if !active {
		t.Error("expected cached active=true")
	}
}

func TestBuildFTSQuery_IdleCache(t *testing.T) {
	db, _ := newTestDB(t)

	expiry := time.Now().Add(time.Hour).Unix()
	_ = db.SetPref("repo_activity:alice/idle", "idle")
	_ = db.SetPref("repo_activity_expires:alice/idle", formatInt(expiry))

	active, err := classifyRepo(context.Background(), db, nil, "alice/idle")
	if err != nil {
		t.Fatalf("classifyRepo: %v", err)
	}
	if active {
		t.Error("expected cached active=false for idle repo")
	}
}

func TestBuildFTSQuery_ExpiredCache(t *testing.T) {
	db, _ := newTestDB(t)

	// Expired cache — classifyRepo should try to re-classify.
	// We verify it doesn't return the stale "active" value by checking it
	// attempts a network call (which will panic with nil client — catch it).
	pastExpiry := time.Now().Add(-time.Hour).Unix()
	_ = db.SetPref("repo_activity:alice/stale", "active")
	_ = db.SetPref("repo_activity_expires:alice/stale", formatInt(pastExpiry))

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic or error when cache expired and client is nil")
		}
	}()
	classifyRepo(context.Background(), db, nil, "alice/stale") //nolint
}

func formatInt(n int64) string {
	return fmt.Sprintf("%d", n)
}
