package audit

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecord_AggregatesSuccessAndFail(t *testing.T) {
	var mu sync.Mutex
	var got string
	l := New(time.Hour, func(_ context.Context, msg string) {
		mu.Lock()
		got = msg
		mu.Unlock()
	})

	l.Record(Entry{UserID: "1", DisplayName: "Alice", Command: "ping", Success: true})
	l.Record(Entry{UserID: "1", DisplayName: "Alice", Command: "ping", Success: true})
	l.Record(Entry{UserID: "1", DisplayName: "Alice", Command: "ping", Success: false})

	l.flush(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(got, "×2") {
		t.Errorf("expected ×2 for successes, got: %q", got)
	}
	if !strings.Contains(got, "❌") {
		t.Errorf("expected ❌ for failure, got: %q", got)
	}
}

func TestRecord_DifferentUsers_SeparateLines(t *testing.T) {
	var mu sync.Mutex
	var got string
	l := New(time.Hour, func(_ context.Context, msg string) {
		mu.Lock()
		got = msg
		mu.Unlock()
	})

	l.Record(Entry{UserID: "1", DisplayName: "Alice", Command: "ping", Success: true})
	l.Record(Entry{UserID: "2", DisplayName: "Bob", Command: "ping", Success: true})

	l.flush(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "Bob") {
		t.Errorf("expected both users in output, got: %q", got)
	}
}

func TestRecord_EmptyWindow_NoFlush(t *testing.T) {
	called := false
	l := New(time.Hour, func(_ context.Context, _ string) {
		called = true
	})
	l.flush(context.Background())
	if called {
		t.Error("flushFn should not be called for empty window")
	}
}

func TestStop_FlushesRemainingEntries(t *testing.T) {
	var mu sync.Mutex
	var got string
	l := New(time.Hour, func(_ context.Context, msg string) {
		mu.Lock()
		got = msg
		mu.Unlock()
	})
	l.Start(context.Background())

	l.Record(Entry{UserID: "1", DisplayName: "Alice", Command: "restart", Success: true})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	l.Stop(ctx)

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(got, "restart") {
		t.Errorf("expected 'restart' in flushed output, got: %q", got)
	}
}

func TestFlushInterval_FiresPeriodically(t *testing.T) {
	var mu sync.Mutex
	flushCount := 0
	l := New(30*time.Millisecond, func(_ context.Context, _ string) {
		mu.Lock()
		flushCount++
		mu.Unlock()
	})
	l.Start(context.Background())

	// Feed one record per tick so each flush window has something to send.
	for range 4 {
		l.Record(Entry{UserID: "1", DisplayName: "Alice", Command: "ping", Success: true})
		time.Sleep(35 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	l.Stop(ctx)

	mu.Lock()
	defer mu.Unlock()
	// 4 records fed across ~140ms with a 30ms interval → expect at least 3 flushes.
	if flushCount < 3 {
		t.Errorf("expected at least 3 periodic flushes, got %d", flushCount)
	}
}

func TestNilLog_NoPanic(t *testing.T) {
	var l *Log
	l.Record(Entry{UserID: "1", DisplayName: "Alice", Command: "ping", Success: true})
	l.Start(context.Background())
	l.Stop(context.Background())
}

func TestFormatLine_SingleSuccess(t *testing.T) {
	b := &bucket{displayName: "Alice", successCount: 1}
	line := formatLine("ping", b)
	if strings.Contains(line, "×") {
		t.Errorf("single success should not show ×, got: %q", line)
	}
	if !strings.Contains(line, "/ping") {
		t.Errorf("expected /ping in line, got: %q", line)
	}
}

func TestFormatLine_MultipleSuccesses(t *testing.T) {
	b := &bucket{displayName: "Alice", successCount: 5}
	line := formatLine("ping", b)
	if !strings.Contains(line, "×5") {
		t.Errorf("expected ×5, got: %q", line)
	}
}
