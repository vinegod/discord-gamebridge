package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/executor"
)

// Scheduler runs configured jobs on cron expressions.
// Jobs fire at the next scheduled time after startup — no state is persisted
// across restarts, so a missed firing is simply skipped.
type Scheduler struct {
	c *cron.Cron
}

// New builds a Scheduler from config and starts it. Returns an error if any
// cron expression is invalid or a referenced executor is not found.
func New(ctx context.Context, schedules []config.ScheduleConfig, reg *executor.Registry) (*Scheduler, error) {
	// WithSeconds enables a 6-field format: second minute hour day month weekday.
	// Standard 5-field expressions remain valid by prepending "0 ".
	c := cron.New(cron.WithSeconds())

	for _, sched := range schedules {
		sched := sched

		ex, err := reg.Get(sched.Executor)
		if err != nil {
			return nil, fmt.Errorf("schedule %q: %w", sched.Name, err)
		}

		s := sched
		if _, err := c.AddFunc(sched.Cron, func() {
			run(ctx, &s, ex)
		}); err != nil {
			return nil, fmt.Errorf("schedule %q: invalid cron expression %q: %w", sched.Name, sched.Cron, err)
		}

		slog.Info("schedule registered", "name", sched.Name, "cron", sched.Cron)
	}

	c.Start()
	return &Scheduler{c: c}, nil
}

// Stop halts the scheduler and waits for any running job to finish.
func (s *Scheduler) Stop() {
	s.c.Stop()
}

func run(ctx context.Context, sched *config.ScheduleConfig, ex executor.Executor) {
	if sched.SkipIfDown {
		if hc, ok := ex.(executor.HealthChecker); ok {
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if !hc.Healthy(checkCtx) {
				slog.Debug("schedule skipped: server is down", "schedule", sched.Name)
				return
			}
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, sched.Timeout)
	defer cancel()

	if _, err := ex.Send(runCtx, sched.Command); err != nil {
		slog.Error("scheduled command failed", "schedule", sched.Name, "error", err)
	} else {
		slog.Debug("scheduled command executed", "schedule", sched.Name)
	}
}
