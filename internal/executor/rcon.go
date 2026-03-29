package executor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorcon/rcon"
)

const (
	rconMaxRetries  = 3
	rconBaseBackoff = 500 * time.Millisecond
)

// RconExecutor sends commands over a persistent RCON TCP connection.
type RconExecutor struct {
	address  string // "host:port"
	password string

	mu   sync.Mutex
	conn *rcon.Conn
}

// Creates an RconExecutor.
func NewRconExecutor(host string, port int, password string) (*RconExecutor, error) {
	e := &RconExecutor{
		address:  fmt.Sprintf("%s:%d", host, port),
		password: password,
	}

	if err := e.connect(); err != nil {
		slog.Info("RCON not connected", "address", e.address)
	}

	return e, nil
}

func (e *RconExecutor) executeWithContext(ctx context.Context, command string) (string, error) {
	type result struct {
		resp string
		err  error
	}

	// Buffer of 1 ensures the goroutine can write and exit even if we stop listening.
	ch := make(chan result, 1)
	conn := e.conn // capture before spawning goroutine
	go func() {
		resp, err := conn.Execute(command)
		ch <- result{resp: resp, err: err}
	}()

	select {
	case <-ctx.Done():
		slog.Warn("RCON timed out, dropping connection", "address", e.address)
		_ = e.conn.Close()
		e.conn = nil
		return "", fmt.Errorf("timeou error: %w", ctx.Err())
	case res := <-ch:
		return res.resp, res.err
	}
}

// Send executes command over RCON and returns the server's response.
// On connection failure it reconnects with exponential backoff before retrying.
func (e *RconExecutor) Send(ctx context.Context, command string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var lastErr error
	for attempt := range rconMaxRetries {
		if e.conn == nil {
			if err := e.connect(); err != nil {
				lastErr = err
				continue
			}

			backoff := rconBaseBackoff * time.Duration(1<<attempt)
			slog.Warn("RCON reconnecting",
				"address", e.address,
				"attempt", attempt+1,
				"backoff", backoff,
			)
			time.Sleep(backoff)
		}

		resp, err := e.executeWithContext(ctx, command)
		if err != nil {
			if ctx.Err() != nil {
				// Context cancelled/timed out — don't retry, caller is already gone.
				return "", err
			}
			slog.Warn("RCON execute failed, will reconnect",
				"address", e.address,
				"error", err,
			)
			// conn already nilled by executeWithContext on timeout,
			// or nil it here for other errors
			if e.conn != nil {
				_ = e.conn.Close()
				e.conn = nil
			}
			lastErr = err
			continue
		}

		return resp, nil
	}

	return "", fmt.Errorf("RCON send failed after %d attempts: %w", rconMaxRetries, lastErr)
}

// Close closes the underlying RCON connection.
// Implements LifecycleExecutor.
func (e *RconExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.conn == nil {
		return nil
	}

	err := e.conn.Close()
	e.conn = nil
	return fmt.Errorf("failed to close rcon: %w", err)
}

// connect establishes a new RCON connection. Must be called with e.mu held.
func (e *RconExecutor) connect() error {
	conn, err := rcon.Dial(e.address, e.password)
	if err != nil {
		return fmt.Errorf("dial %s: %w", e.address, err)
	}
	e.conn = conn
	return nil
}
