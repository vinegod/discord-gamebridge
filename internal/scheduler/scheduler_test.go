package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/executor"
)

// stubExecutor counts Send calls and optionally reports unhealthy.
type stubExecutor struct {
	calls   atomic.Int32
	healthy bool
}

func (s *stubExecutor) Send(_ context.Context, _ string, _ ...string) (string, error) {
	s.calls.Add(1)
	return "", nil
}

func (s *stubExecutor) Healthy(_ context.Context) bool {
	return s.healthy
}

func newReg(ex executor.Executor) *executor.Registry {
	reg := executor.NewRegistry()
	reg.Register("tmux", ex)
	return reg
}

func TestScheduler_FiresOnCron(t *testing.T) {
	ex := &stubExecutor{healthy: true}
	reg := newReg(ex)

	sched, err := New(context.Background(), []config.ScheduleConfig{
		{Name: "test", Cron: "* * * * * *", Executor: "tmux", Command: "save", Timeout: time.Second},
	}, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sched.Stop()

	time.Sleep(2100 * time.Millisecond)
	if ex.calls.Load() < 1 {
		t.Error("expected at least one Send call within 2s")
	}
}

func TestScheduler_SkipIfDown_HealthyExecutor_Runs(t *testing.T) {
	ex := &stubExecutor{healthy: true}
	reg := newReg(ex)

	sched, err := New(context.Background(), []config.ScheduleConfig{
		{
			Name: "test", Cron: "* * * * * *", Executor: "tmux", Command: "save",
			Timeout: time.Second, SkipIfDown: true,
		},
	}, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sched.Stop()

	time.Sleep(2100 * time.Millisecond)
	if ex.calls.Load() < 1 {
		t.Error("expected Send to be called when server is healthy")
	}
}

func TestScheduler_SkipIfDown_UnhealthyExecutor_Skips(t *testing.T) {
	ex := &stubExecutor{healthy: false}
	reg := newReg(ex)

	sched, err := New(context.Background(), []config.ScheduleConfig{
		{
			Name: "test", Cron: "* * * * * *", Executor: "tmux", Command: "save",
			Timeout: time.Second, SkipIfDown: true,
		},
	}, reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sched.Stop()

	time.Sleep(2100 * time.Millisecond)
	if ex.calls.Load() != 0 {
		t.Errorf("expected no Send calls when server is down, got %d", ex.calls.Load())
	}
}

func TestScheduler_InvalidCron_ReturnsError(t *testing.T) {
	ex := &stubExecutor{}
	reg := newReg(ex)

	_, err := New(context.Background(), []config.ScheduleConfig{
		{Name: "bad", Cron: "not-a-cron", Executor: "tmux", Command: "save", Timeout: time.Second},
	}, reg)
	if err == nil {
		t.Error("expected error for invalid cron expression, got nil")
	}
}

func TestScheduler_UnknownExecutor_ReturnsError(t *testing.T) {
	reg := executor.NewRegistry()

	_, err := New(context.Background(), []config.ScheduleConfig{
		{Name: "test", Cron: "* * * * *", Executor: "missing", Command: "save", Timeout: time.Second},
	}, reg)
	if err == nil {
		t.Error("expected error for unknown executor, got nil")
	}
}

func TestScheduler_NoSchedules_Starts(t *testing.T) {
	sched, err := New(context.Background(), nil, executor.NewRegistry())
	if err != nil {
		t.Fatalf("unexpected error with empty schedules: %v", err)
	}
	sched.Stop()
}
