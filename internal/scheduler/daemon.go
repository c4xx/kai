// Package scheduler manages the cron-based kai daemon loop.
package scheduler

import (
	"context"
	"log"

	"github.com/c4xx/kai/internal/config"
	"github.com/c4xx/kai/internal/core"
	"github.com/c4xx/kai/internal/memory"
	"github.com/robfig/cron/v3"
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

// Start adds the scheduled job and starts the cron runner.
func (d *Daemon) Start(ctx context.Context) error {
	_, err := d.cron.AddFunc(d.cfg.Schedule, func() {
		if err := core.Run(ctx, d.cfg, d.db, "github_summary"); err != nil {
			log.Printf("kai: scheduled run failed: %v", err)
		}
	})
	if err != nil {
		return err
	}
	d.cron.Start()
	log.Printf("kai: daemon started, schedule: %s", d.cfg.Schedule)
	return nil
}

// Stop gracefully stops the scheduler.
func (d *Daemon) Stop() {
	d.cron.Stop()
}
