// Package scheduler manages the cron-based kai daemon loop.
package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c4xx/kai/internal/config"
	"github.com/c4xx/kai/internal/core"
	"github.com/c4xx/kai/internal/memory"
	"github.com/c4xx/kai/internal/notify"
	"github.com/c4xx/kai/internal/safety"
	"github.com/google/go-github/v60/github"
	"github.com/robfig/cron/v3"
	"golang.org/x/oauth2"
)

const (
	activePollInterval = 15 * time.Minute
	idlePollInterval   = 4 * time.Hour
	classifyTTL        = 24 * time.Hour
)

// Daemon wraps the cron scheduler and daemon lifecycle.
type Daemon struct {
	cfg  *config.Config
	db   *memory.DB
	cron *cron.Cron
}

// New creates a new Daemon.
func New(cfg *config.Config, db *memory.DB) *Daemon {
	return &Daemon{
		cfg:  cfg,
		db:   db,
		cron: cron.New(),
	}
}

// Start adds the scheduled job and starts the cron runner, then kicks off the
// GitHub polling loop.
func (d *Daemon) Start(ctx context.Context) error {
	_, err := d.cron.AddFunc(d.cfg.Schedule, func() {
		if err := core.Run(ctx, d.cfg, d.db, "github_summary"); err != nil {
			log.Printf("kai: scheduled run failed: %v", err)
		}
	})
	if err != nil {
		return err
	}

	// Standup reminder cron: fire on weekdays at team.standup_reminder time.
	if d.cfg.Team.StandupReminder != "" {
		cronExpr, err := standupReminderCron(d.cfg.Team.StandupReminder)
		if err != nil {
			log.Printf("kai: invalid standup_reminder %q: %v (skipping)", d.cfg.Team.StandupReminder, err)
		} else {
			_, err = d.cron.AddFunc(cronExpr, func() {
				d.sendStandupReminders(ctx)
			})
			if err != nil {
				log.Printf("kai: failed to register standup reminder cron: %v", err)
			}
		}
	}
	d.cron.Start()
	log.Printf("kai: daemon started, schedule: %s", d.cfg.Schedule)

	if len(d.cfg.WatchRepos) > 0 {
		go d.pollLoop(ctx)
	}
	return nil
}

// Stop gracefully stops the scheduler.
func (d *Daemon) Stop() {
	d.cron.Stop()
}

// pollLoop continuously polls watched repos at their classified interval
// (active = 15 min, idle = 4 h) and triggers a github_summary run when
// active repos have new commits.
func (d *Daemon) pollLoop(ctx context.Context) {
	// Build initial per-repo timers using cached classifications.
	type repoState struct {
		repo     string
		nextPoll time.Time
	}
	states := make([]repoState, len(d.cfg.WatchRepos))
	for i, r := range d.cfg.WatchRepos {
		states[i] = repoState{repo: r, nextPoll: time.Now()}
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Expire stale pending actions once on startup, then hourly.
	expireTicker := time.NewTicker(time.Hour)
	defer expireTicker.Stop()
	expireStale(d.cfg, d.db)

	ghClient := newGitHubClient(ctx, d.cfg.GitHubToken)

	// runInFlight ensures at most one poll-triggered core.Run at a time,
	// preventing multiple active repos from racing and overshooting the daily token budget.
	var runInFlight atomic.Bool

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for i, s := range states {
				if now.Before(s.nextPoll) {
					continue
				}
				active, err := classifyRepo(ctx, d.db, ghClient, s.repo)
				if err != nil {
					log.Printf("kai: poll classify %s: %v", s.repo, err)
					// Back off and retry later.
					states[i].nextPoll = now.Add(idlePollInterval)
					continue
				}
				if active {
					states[i].nextPoll = now.Add(activePollInterval)
					log.Printf("kai: %s is active, triggering github_summary", s.repo)
					if runInFlight.CompareAndSwap(false, true) {
						go func() {
							defer runInFlight.Store(false)
							if err := core.Run(ctx, d.cfg, d.db, "github_summary"); err != nil {
								log.Printf("kai: poll-triggered run failed: %v", err)
							}
						}()
					} else {
						log.Printf("kai: %s active but run already in flight, skipping", s.repo)
					}
				} else {
					states[i].nextPoll = now.Add(idlePollInterval)
				}
			}
		case <-expireTicker.C:
			expireStale(d.cfg, d.db)
		}
	}
}

// expireStale auto-aborts pending actions older than 7 days.
func expireStale(cfg *config.Config, db *memory.DB) {
	gate := safety.NewGate(cfg.DataDir, cfg.Trust.StateChange)
	expired, err := gate.ExpireStale(7 * 24 * time.Hour)
	if err != nil {
		log.Printf("kai: expireStale: %v", err)
		return
	}
	for _, id := range expired {
		db.LogActionAbort(id, "auto-expired")
		log.Printf("kai: auto-expired stale pending action %s", id)
	}
}

// classifyRepo returns true if the repo had commits in the last 24 hours.
// Result is cached in the preferences table with a 24h TTL.
func classifyRepo(ctx context.Context, db *memory.DB, client *github.Client, repoSpec string) (bool, error) {
	activityKey := "repo_activity:" + repoSpec
	expiryKey := "repo_activity_expires:" + repoSpec

	// Check cache.
	expiryStr, err := db.GetPref(expiryKey)
	if err == nil && expiryStr != "" {
		var expiryTS int64
		if _, err := fmt.Sscanf(expiryStr, "%d", &expiryTS); err == nil {
			if time.Now().Unix() < expiryTS {
				val, _ := db.GetPref(activityKey)
				return val == "active", nil
			}
		}
	}

	// Cache miss or expired — query GitHub.
	parts := strings.SplitN(repoSpec, "/", 2)
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid repo spec: %s", repoSpec)
	}
	owner, repo := parts[0], parts[1]

	since := time.Now().Add(-24 * time.Hour)
	commits, _, err := client.Repositories.ListCommits(ctx, owner, repo, &github.CommitsListOptions{
		Since:       since,
		ListOptions: github.ListOptions{PerPage: 1},
	})
	if err != nil {
		return false, fmt.Errorf("listing commits: %w", err)
	}

	active := len(commits) > 0
	classification := "idle"
	if active {
		classification = "active"
	}

	expiry := fmt.Sprintf("%d", time.Now().Add(classifyTTL).Unix())
	_ = db.SetPref(activityKey, classification)
	_ = db.SetPref(expiryKey, expiry)

	return active, nil
}

func newGitHubClient(ctx context.Context, token string) *github.Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

// standupReminderCron converts "HH:MM" to a weekday-only cron expression "MM HH * * 1-5".
func standupReminderCron(hhmm string) (string, error) {
	parts := strings.SplitN(hhmm, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("expected HH:MM format")
	}
	h, err1 := fmt.Sscanf(parts[0], "%d", new(int))
	m, err2 := fmt.Sscanf(parts[1], "%d", new(int))
	if h != 1 || m != 1 || err1 != nil || err2 != nil {
		return "", fmt.Errorf("invalid HH:MM: %q", hhmm)
	}
	return fmt.Sprintf("%s %s * * 1-5", strings.TrimSpace(parts[1]), strings.TrimSpace(parts[0])), nil
}

// sendStandupReminders queries today's standups and notifies members who haven't submitted.
func (d *Daemon) sendStandupReminders(ctx context.Context) {
	if len(d.cfg.Team.Members) == 0 {
		return
	}
	today := time.Now().Format("2006-01-02")
	submitted, err := d.db.StandupsForDate(today)
	if err != nil {
		log.Printf("kai: standup reminder: querying standups: %v", err)
		return
	}
	submittedSet := make(map[string]bool, len(submitted))
	for _, s := range submitted {
		submittedSet[s.Member] = true
	}
	for _, member := range d.cfg.Team.Members {
		if !submittedSet[member] {
			log.Printf("kai: standup reminder: %s has not submitted today", member)
			notify.Send("kai: standup reminder",
				fmt.Sprintf("%s has not submitted a standup today. Run `kai standup submit --member %s`", member, member))
		}
	}
}

