package memory

import (
	"context"
	"sync"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestInsertAndGetRun(t *testing.T) {
	db := openTestDB(t)

	r := &Run{
		ID:     "run-1",
		Job:    "github_summary",
		TS:     time.Now().Unix(),
		Status: "in_progress",
	}
	if err := db.InsertRun(r); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	got, err := db.GetRun("run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got == nil {
		t.Fatal("expected run, got nil")
	}
	if got.Status != "in_progress" {
		t.Errorf("expected in_progress, got %s", got.Status)
	}
}

func TestUpdateRunStatus(t *testing.T) {
	db := openTestDB(t)
	r := &Run{ID: "run-2", Job: "github_summary", TS: time.Now().Unix(), Status: "pending"}
	if err := db.InsertRun(r); err != nil {
		t.Fatal(err)
	}

	summary := "morning briefing"
	tokens := int64(1234)
	if err := db.UpdateRunStatus("run-2", "completed", &summary, &tokens); err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}

	got, err := db.GetRun("run-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "completed" {
		t.Errorf("expected completed, got %s", got.Status)
	}
	if got.Summary == nil || *got.Summary != summary {
		t.Errorf("unexpected summary: %v", got.Summary)
	}
}

func TestInsertAction(t *testing.T) {
	db := openTestDB(t)
	r := &Run{ID: "run-3", Job: "github_summary", TS: time.Now().Unix(), Status: "in_progress"}
	if err := db.InsertRun(r); err != nil {
		t.Fatal(err)
	}

	a := &Action{
		ID:          "action-1",
		RunID:       "run-3",
		Tool:        "file_read",
		Params:      `{"path":"/etc/hosts"}`,
		TS:          time.Now().Unix(),
		BlastRadius: "READ_ONLY",
		Confirmed:   0,
	}
	if err := db.InsertAction(a); err != nil {
		t.Fatalf("InsertAction: %v", err)
	}

	got, err := db.GetRunForAction("action-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != "run-3" {
		t.Errorf("expected run-3, got %v", got)
	}
}

func TestLogActionAbort(t *testing.T) {
	db := openTestDB(t)
	r := &Run{ID: "run-4", Job: "github_summary", TS: time.Now().Unix(), Status: "pending"}
	if err := db.InsertRun(r); err != nil {
		t.Fatal(err)
	}
	a := &Action{
		ID: "action-2", RunID: "run-4", Tool: "bash_exec",
		Params: `{}`, TS: time.Now().Unix(), BlastRadius: "STATE_CHANGE",
	}
	if err := db.InsertAction(a); err != nil {
		t.Fatal(err)
	}
	if err := db.LogActionAbort("action-2", "daemon-restarted"); err != nil {
		t.Fatalf("LogActionAbort: %v", err)
	}

	actions, err := db.ActionsForRun("run-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Output == nil || *actions[0].Output != "aborted: daemon-restarted" {
		t.Errorf("unexpected output: %v", actions[0].Output)
	}
}

func TestConcurrentWrites(t *testing.T) {
	db := openTestDB(t)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r := &Run{
				ID:     "run-concurrent-" + string(rune('A'+n)),
				Job:    "github_summary",
				TS:     time.Now().Unix(),
				Status: "pending",
			}
			if err := db.InsertRun(r); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent write error: %v", err)
	}
}

func TestFTS5Trigger(t *testing.T) {
	db := openTestDB(t)
	summary := "PR #42 needs review from alice"
	r := &Run{ID: "run-fts", Job: "github_summary", TS: time.Now().Unix(), Status: "pending"}
	if err := db.InsertRun(r); err != nil {
		t.Fatal(err)
	}
	tokens := int64(100)
	if err := db.UpdateRunStatus("run-fts", "completed", &summary, &tokens); err != nil {
		t.Fatal(err)
	}

	// FTS5 trigger fires on UPDATE — search for "alice"
	rows, err := db.readDB.Query(`SELECT rowid FROM runs_fts WHERE runs_fts MATCH 'alice'`)
	if err != nil {
		t.Fatalf("FTS5 query: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 FTS5 result, got %d", count)
	}
}

func TestPreferences(t *testing.T) {
	db := openTestDB(t)

	val, err := db.GetPref("theme")
	if err != nil {
		t.Fatal(err)
	}
	if val != "" {
		t.Errorf("expected empty, got %s", val)
	}

	if err := db.SetPref("theme", "dark"); err != nil {
		t.Fatal(err)
	}
	val, err = db.GetPref("theme")
	if err != nil {
		t.Fatal(err)
	}
	if val != "dark" {
		t.Errorf("expected dark, got %s", val)
	}

	// upsert
	if err := db.SetPref("theme", "light"); err != nil {
		t.Fatal(err)
	}
	val, _ = db.GetPref("theme")
	if val != "light" {
		t.Errorf("expected light after upsert, got %s", val)
	}
}

func TestInsertStandup_HappyPath(t *testing.T) {
	db := openTestDB(t)

	s := &Standup{
		Member:  "alice",
		Date:    "2026-04-07",
		Done:    "finished login page",
		Today:   "starting auth module",
		Blocked: "",
		TS:      1000,
	}
	if err := db.InsertStandup(s); err != nil {
		t.Fatalf("InsertStandup: %v", err)
	}

	rows, err := db.StandupsForDate("2026-04-07")
	if err != nil {
		t.Fatalf("StandupsForDate: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 standup, got %d", len(rows))
	}
	if rows[0].Member != "alice" {
		t.Errorf("expected alice, got %s", rows[0].Member)
	}
	if rows[0].Done != "finished login page" {
		t.Errorf("unexpected Done: %s", rows[0].Done)
	}
}

func TestInsertStandup_Upsert(t *testing.T) {
	db := openTestDB(t)

	s1 := &Standup{Member: "bob", Date: "2026-04-07", Done: "first write", Today: "plan A", TS: 1000}
	if err := db.InsertStandup(s1); err != nil {
		t.Fatalf("InsertStandup first: %v", err)
	}

	// Second write to same member+date — last-write-wins.
	s2 := &Standup{Member: "bob", Date: "2026-04-07", Done: "second write", Today: "plan B", TS: 2000}
	if err := db.InsertStandup(s2); err != nil {
		t.Fatalf("InsertStandup second: %v", err)
	}

	rows, err := db.StandupsForDate("2026-04-07")
	if err != nil {
		t.Fatalf("StandupsForDate: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", len(rows))
	}
	if rows[0].Done != "second write" {
		t.Errorf("expected second write wins, got %s", rows[0].Done)
	}
}

func TestStandupsForDate_Empty(t *testing.T) {
	db := openTestDB(t)

	rows, err := db.StandupsForDate("2026-04-01")
	if err != nil {
		t.Fatalf("StandupsForDate: %v", err)
	}
	// Must return empty slice, not nil.
	if rows == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}

func TestMemberStandupHistory_CrossDay(t *testing.T) {
	db := openTestDB(t)

	// Insert two days for alice — most recent first after query.
	s1 := &Standup{Member: "alice", Date: "2026-04-06", Done: "login page done", Today: "auth module", TS: 900}
	s2 := &Standup{Member: "alice", Date: "2026-04-07", Done: "auth module done", Today: "permissions", TS: 1000}
	if err := db.InsertStandup(s1); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertStandup(s2); err != nil {
		t.Fatal(err)
	}

	hist, err := db.MemberStandupHistory("alice", 2)
	if err != nil {
		t.Fatalf("MemberStandupHistory: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 records, got %d", len(hist))
	}
	// ORDER BY date DESC — hist[0] should be the most recent.
	if hist[0].Date != "2026-04-07" {
		t.Errorf("expected hist[0] = 2026-04-07 (most recent), got %s", hist[0].Date)
	}
	if hist[1].Date != "2026-04-06" {
		t.Errorf("expected hist[1] = 2026-04-06 (yesterday), got %s", hist[1].Date)
	}
}

func TestMemberStandupHistory_Empty(t *testing.T) {
	db := openTestDB(t)

	hist, err := db.MemberStandupHistory("nobody", 5)
	if err != nil {
		t.Fatalf("MemberStandupHistory: %v", err)
	}
	if hist == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(hist) != 0 {
		t.Fatalf("expected 0 records, got %d", len(hist))
	}
}
