// Package audit aggregates command invocations and periodically flushes a
// summary to a Discord channel. Identical (user, command) pairs within the
// flush window are collapsed into a single line with a repeat count.
package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry records one command invocation.
type Entry struct {
	UserID      string
	DisplayName string
	Command     string
	Success     bool
}

type entryKey struct {
	userID  string
	command string
}

type bucket struct {
	displayName  string
	successCount int
	failCount    int
}

// FlushFunc is called with the formatted audit summary when the window closes.
type FlushFunc func(ctx context.Context, message string)

// Log batches command audit entries and flushes them on a fixed interval.
// A nil Log silently ignores all calls — callers need not guard with nil checks.
type Log struct {
	mu       sync.Mutex
	buckets  map[entryKey]*bucket
	flushFn  FlushFunc
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// New creates a Log that calls flushFn every interval. Call Start to begin
// the flush loop and Stop to shut it down and flush the final window.
func New(interval time.Duration, fn FlushFunc) *Log {
	return &Log{
		buckets:  make(map[entryKey]*bucket),
		flushFn:  fn,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// SetFlushFunc replaces the flush function. Must be called before Start.
func (l *Log) SetFlushFunc(fn FlushFunc) {
	if l == nil {
		return
	}
	l.flushFn = fn
}

// Start begins the background flush goroutine. The ctx is used for periodic
// flushes; it should be the operational context (not the shutdown context).
func (l *Log) Start(ctx context.Context) {
	if l == nil {
		return
	}
	go l.loop(ctx)
}

// Stop signals the loop to exit, then performs a final flush using ctx.
// Use a fresh (non-cancelled) context so the final flush is not skipped.
func (l *Log) Stop(ctx context.Context) {
	if l == nil {
		return
	}
	close(l.stopCh)
	<-l.doneCh
	l.flush(ctx)
}

// Record adds one invocation to the current window's buffer.
func (l *Log) Record(e Entry) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	key := entryKey{userID: e.UserID, command: e.Command}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{displayName: e.DisplayName}
		l.buckets[key] = b
	}
	b.displayName = e.DisplayName
	if e.Success {
		b.successCount++
	} else {
		b.failCount++
	}
}

func (l *Log) loop(ctx context.Context) {
	defer close(l.doneCh)
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.flush(ctx)
		case <-l.stopCh:
			return // Stop() calls flush with its own context
		}
	}
}

func (l *Log) flush(ctx context.Context) {
	l.mu.Lock()
	buckets := l.buckets
	l.buckets = make(map[entryKey]*bucket)
	l.mu.Unlock()

	if len(buckets) == 0 || l.flushFn == nil {
		return
	}

	l.flushFn(ctx, format(buckets))
}

func format(buckets map[entryKey]*bucket) string {
	type line struct {
		key entryKey
		b   *bucket
	}
	lines := make([]line, 0, len(buckets))
	for k, b := range buckets {
		lines = append(lines, line{k, b})
	}
	// Stable order: sort by displayName then command.
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].b.displayName != lines[j].b.displayName {
			return lines[i].b.displayName < lines[j].b.displayName
		}
		return lines[i].key.command < lines[j].key.command
	})

	var sb strings.Builder
	sb.WriteString("**Command Audit**\n")
	for _, l := range lines {
		sb.WriteString(formatLine(l.key.command, l.b))
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatLine(command string, b *bucket) string {
	var parts []string
	if b.successCount > 0 {
		if b.successCount == 1 {
			parts = append(parts, fmt.Sprintf("`/%s`", command))
		} else {
			parts = append(parts, fmt.Sprintf("`/%s` ×%d", command, b.successCount))
		}
	}
	if b.failCount > 0 {
		if b.failCount == 1 {
			parts = append(parts, fmt.Sprintf("`/%s` ❌", command))
		} else {
			parts = append(parts, fmt.Sprintf("`/%s` ❌×%d", command, b.failCount))
		}
	}
	return fmt.Sprintf("• **%s**: %s", b.displayName, strings.Join(parts, "  "))
}
