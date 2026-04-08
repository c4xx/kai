// Package memory manages kai's SQLite database: schema, write serialization, and queries.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS runs (
  run_rowid       INTEGER PRIMARY KEY AUTOINCREMENT,
  id              TEXT UNIQUE NOT NULL,
  job             TEXT NOT NULL,
  ts              INTEGER NOT NULL,
  status          TEXT NOT NULL DEFAULT 'pending',
  summary         TEXT,
  tokens_used     INTEGER,
  active_days     INTEGER DEFAULT 0,
  briefing_opened INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS actions (
  id           TEXT PRIMARY KEY,
  run_id       TEXT REFERENCES runs(id),
  tool         TEXT NOT NULL,
  params       TEXT NOT NULL,
  output       TEXT,
  ts           INTEGER NOT NULL,
  blast_radius TEXT NOT NULL,
  confirmed    INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS preferences (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS standups (
  standup_rowid  INTEGER PRIMARY KEY AUTOINCREMENT,
  member         TEXT NOT NULL,
  date           TEXT NOT NULL,
  done           TEXT,
  today          TEXT,
  blocked        TEXT,
  ts             INTEGER NOT NULL,
  UNIQUE(member, date)
);
CREATE INDEX IF NOT EXISTS standups_date_member ON standups(date, member);

CREATE INDEX IF NOT EXISTS idx_runs_ts        ON runs(ts);
CREATE INDEX IF NOT EXISTS idx_actions_run_id ON actions(run_id);
CREATE INDEX IF NOT EXISTS idx_actions_ts     ON actions(ts);

CREATE VIRTUAL TABLE IF NOT EXISTS runs_fts USING fts5(summary, content='runs', content_rowid='run_rowid');

CREATE TRIGGER IF NOT EXISTS runs_ai AFTER INSERT ON runs BEGIN
  INSERT INTO runs_fts(rowid, summary) VALUES (new.run_rowid, new.summary);
END;
CREATE TRIGGER IF NOT EXISTS runs_ad AFTER DELETE ON runs BEGIN
  INSERT INTO runs_fts(runs_fts, rowid, summary) VALUES ('delete', old.run_rowid, old.summary);
END;
CREATE TRIGGER IF NOT EXISTS runs_au AFTER UPDATE ON runs BEGIN
  INSERT INTO runs_fts(runs_fts, rowid, summary) VALUES ('delete', old.run_rowid, old.summary);
  INSERT INTO runs_fts(rowid, summary) VALUES (new.run_rowid, new.summary);
END;
`

// writeOp is a unit of work sent to the write-serializer goroutine.
type writeOp struct {
	fn   func(*sql.Tx) error
	done chan error
}

// DB wraps two SQLite connections: one write-serializer and one read-only pool.
type DB struct {
	writeDB *sql.DB // single-connection writer
	readDB  *sql.DB // read-only connection pool
	writes  chan writeOp
	cancel  context.CancelFunc
}

// Open opens (or creates) the kai.db in dataDir and starts the write serializer.
func Open(ctx context.Context, dataDir string) (*DB, error) {
	dbPath := filepath.Join(dataDir, "kai.db")
	dsn := "file:" + dbPath + "?_foreign_keys=on&_journal_mode=WAL"

	writeDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening write db: %w", err)
	}
	writeDB.SetMaxOpenConns(1)

	if _, err := writeDB.ExecContext(ctx, schema); err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	readDB, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_foreign_keys=on")
	if err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("opening read db: %w", err)
	}

	ctx2, cancel := context.WithCancel(ctx)
	d := &DB{
		writeDB: writeDB,
		readDB:  readDB,
		writes:  make(chan writeOp, 64),
		cancel:  cancel,
	}
	go d.serializer(ctx2)
	return d, nil
}

// Close shuts down the write serializer and closes both connections.
func (d *DB) Close() error {
	d.cancel()
	d.writeDB.Close()
	return d.readDB.Close()
}

// serializer drains the writes channel sequentially.
func (d *DB) serializer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case op := <-d.writes:
			op.done <- d.execTx(op.fn)
		}
	}
}

func (d *DB) execTx(fn func(*sql.Tx) error) error {
	tx, err := d.writeDB.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// write submits a write operation to the serializer and waits for it to complete.
func (d *DB) write(fn func(*sql.Tx) error) error {
	op := writeOp{fn: fn, done: make(chan error, 1)}
	d.writes <- op
	return <-op.done
}

// --- Run operations ---

// Run represents a row in the runs table.
type Run struct {
	RowID          int64
	ID             string
	Job            string
	TS             int64
	Status         string
	Summary        *string
	TokensUsed     *int64
	ActiveDays     int
	BriefingOpened int
}

// InsertRun inserts a new run record.
func (d *DB) InsertRun(r *Run) error {
	return d.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO runs (id, job, ts, status) VALUES (?, ?, ?, ?)`,
			r.ID, r.Job, r.TS, r.Status,
		)
		return err
	})
}

// UpdateRunStatus updates run status (and optionally summary + tokens).
func (d *DB) UpdateRunStatus(id, status string, summary *string, tokensUsed *int64) error {
	return d.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`UPDATE runs SET status=?, summary=?, tokens_used=? WHERE id=?`,
			status, summary, tokensUsed, id,
		)
		return err
	})
}

// MarkBriefingOpened increments briefing_opened for a run.
func (d *DB) MarkBriefingOpened(id string) error {
	return d.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE runs SET briefing_opened=1 WHERE id=?`, id)
		return err
	})
}

// GetRun returns a single run by ID.
func (d *DB) GetRun(id string) (*Run, error) {
	row := d.readDB.QueryRow(
		`SELECT run_rowid, id, job, ts, status, summary, tokens_used, active_days, briefing_opened FROM runs WHERE id=?`, id,
	)
	return scanRun(row)
}

// GetRunForAction returns the run that owns the given action ID.
func (d *DB) GetRunForAction(actionID string) (*Run, error) {
	row := d.readDB.QueryRow(
		`SELECT r.run_rowid, r.id, r.job, r.ts, r.status, r.summary, r.tokens_used, r.active_days, r.briefing_opened
		 FROM runs r JOIN actions a ON a.run_id = r.id WHERE a.id = ?`, actionID,
	)
	return scanRun(row)
}

// LatestRuns returns the most recent N runs ordered by ts desc.
func (d *DB) LatestRuns(limit int) ([]*Run, error) {
	rows, err := d.readDB.Query(
		`SELECT run_rowid, id, job, ts, status, summary, tokens_used, active_days, briefing_opened FROM runs ORDER BY ts DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRun(s scanner) (*Run, error) {
	var r Run
	err := s.Scan(
		&r.RowID, &r.ID, &r.Job, &r.TS, &r.Status,
		&r.Summary, &r.TokensUsed, &r.ActiveDays, &r.BriefingOpened,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &r, err
}

// --- Action operations ---

// Action represents a row in the actions table.
type Action struct {
	ID          string
	RunID       string
	Tool        string
	Params      string
	Output      *string
	TS          int64
	BlastRadius string
	Confirmed   int
}

// InsertAction inserts a new action record.
func (d *DB) InsertAction(a *Action) error {
	return d.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO actions (id, run_id, tool, params, output, ts, blast_radius, confirmed)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.RunID, a.Tool, a.Params, a.Output, a.TS, a.BlastRadius, a.Confirmed,
		)
		return err
	})
}

// LogActionAbort marks an action as aborted (confirmed=0) with a note in output.
func (d *DB) LogActionAbort(id, reason string) error {
	return d.write(func(tx *sql.Tx) error {
		note := "aborted: " + reason
		_, err := tx.Exec(
			`UPDATE actions SET output=?, confirmed=0 WHERE id=?`,
			note, id,
		)
		return err
	})
}

// ActionsForRun returns all actions for a given run ID, ordered by ts asc.
func (d *DB) ActionsForRun(runID string) ([]*Action, error) {
	rows, err := d.readDB.Query(
		`SELECT id, run_id, tool, params, output, ts, blast_radius, confirmed FROM actions WHERE run_id=? ORDER BY ts`, runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var actions []*Action
	for rows.Next() {
		var a Action
		if err := rows.Scan(&a.ID, &a.RunID, &a.Tool, &a.Params, &a.Output, &a.TS, &a.BlastRadius, &a.Confirmed); err != nil {
			return nil, err
		}
		actions = append(actions, &a)
	}
	return actions, rows.Err()
}

// RecentActions returns recent actions across all runs, ordered by ts desc.
func (d *DB) RecentActions(limit int) ([]*Action, error) {
	rows, err := d.readDB.Query(
		`SELECT id, run_id, tool, params, output, ts, blast_radius, confirmed FROM actions ORDER BY ts DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var actions []*Action
	for rows.Next() {
		var a Action
		if err := rows.Scan(&a.ID, &a.RunID, &a.Tool, &a.Params, &a.Output, &a.TS, &a.BlastRadius, &a.Confirmed); err != nil {
			return nil, err
		}
		actions = append(actions, &a)
	}
	return actions, rows.Err()
}

// --- Retention metric ---

// RetentionStats returns the composite retention metric for the trailing 30 days.
type RetentionStats struct {
	ActiveDays int
	ReadCount  int
}

func (d *DB) RetentionStats() (*RetentionStats, error) {
	row := d.readDB.QueryRow(`
		SELECT
		  COUNT(DISTINCT date(ts, 'unixepoch', 'localtime')) as active_days,
		  SUM(CASE WHEN briefing_opened=1 THEN 1 ELSE 0 END) as read_count
		FROM runs WHERE ts > strftime('%s', 'now', '-30 days') AND status='completed'
	`)
	var s RetentionStats
	if err := row.Scan(&s.ActiveDays, &s.ReadCount); err != nil {
		return nil, err
	}
	return &s, nil
}

// --- Preferences ---

// GetPref returns a preference value by key. Returns ("", nil) if not set.
func (d *DB) GetPref(key string) (string, error) {
	var val string
	err := d.readDB.QueryRow(`SELECT value FROM preferences WHERE key=?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// SetPref upserts a preference value.
func (d *DB) SetPref(key, value string) error {
	return d.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO preferences (key, value, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			key, value, time.Now().Unix(),
		)
		return err
	})
}

// SearchSummaries performs FTS5 full-text search over run summaries.
// Returns up to limit matching runs ordered by relevance.
func (d *DB) SearchSummaries(query string, limit int) ([]*Run, error) {
	rows, err := d.readDB.Query(`
		SELECT r.run_rowid, r.id, r.job, r.ts, r.status, r.summary, r.tokens_used, r.active_days, r.briefing_opened
		FROM runs_fts
		JOIN runs r ON runs_fts.rowid = r.run_rowid
		WHERE runs_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}


// --- Standup operations ---

// Standup represents one member's daily standup record.
type Standup struct {
	Member  string
	Date    string // YYYY-MM-DD
	Done    string
	Today   string
	Blocked string
	TS      int64
}

// InsertStandup upserts a standup record (ON CONFLICT REPLACE — last-write-wins per member+date).
func (d *DB) InsertStandup(s *Standup) error {
	return d.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO standups (member, date, done, today, blocked, ts)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(member, date) DO UPDATE SET
			   done=excluded.done, today=excluded.today,
			   blocked=excluded.blocked, ts=excluded.ts`,
			s.Member, s.Date, s.Done, s.Today, s.Blocked, s.TS,
		)
		return err
	})
}

// StandupsForDate returns all standups for the given date (YYYY-MM-DD).
// Returns an empty slice (not nil) when no standups exist for the date.
func (d *DB) StandupsForDate(date string) ([]*Standup, error) {
	rows, err := d.readDB.Query(
		`SELECT member, date, COALESCE(done,''), COALESCE(today,''), COALESCE(blocked,''), ts FROM standups WHERE date = ? ORDER BY member`,
		date,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []*Standup{}
	for rows.Next() {
		var s Standup
		if err := rows.Scan(&s.Member, &s.Date, &s.Done, &s.Today, &s.Blocked, &s.TS); err != nil {
			return nil, err
		}
		result = append(result, &s)
	}
	return result, rows.Err()
}

// MemberStandupHistory returns the last N days of standups for a member, most recent first.
func (d *DB) MemberStandupHistory(member string, days int) ([]*Standup, error) {
	rows, err := d.readDB.Query(
		`SELECT member, date, COALESCE(done,''), COALESCE(today,''), COALESCE(blocked,''), ts FROM standups
		 WHERE member = ? ORDER BY date DESC LIMIT ?`,
		member, days,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []*Standup{}
	for rows.Next() {
		var s Standup
		if err := rows.Scan(&s.Member, &s.Date, &s.Done, &s.Today, &s.Blocked, &s.TS); err != nil {
			return nil, err
		}
		result = append(result, &s)
	}
	return result, rows.Err()
}

// TokensUsedToday returns total tokens_used from completed runs today (local time).
func (d *DB) TokensUsedToday() (int64, error) {
	var total sql.NullInt64
	err := d.readDB.QueryRow(`
		SELECT SUM(tokens_used)
		FROM runs
		WHERE status='completed'
		  AND date(ts, 'unixepoch', 'localtime') = date('now', 'localtime')
	`).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}
