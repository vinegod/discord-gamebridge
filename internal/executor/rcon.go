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
		slog.Info("RCON connected", "address", e.address)
	}

	return e, nil
}

// Send executes command over RCON and returns the server's response.
// On connection failure it reconnects with exponential backoff before retrying.
func (e *RconExecutor) Send(_ context.Context, command string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var lastErr error
	for attempt := range rconMaxRetries {
		if e.conn == nil {
			backoff := rconBaseBackoff * time.Duration(1<<attempt)
			slog.Warn("RCON reconnecting",
				"address", e.address,
				"attempt", attempt+1,
				"backoff", backoff,
			)
			time.Sleep(backoff)

			if err := e.connect(); err != nil {
				lastErr = fmt.Errorf("reconnect attempt %d: %w", attempt+1, err)
				continue
			}
		}

		resp, err := e.conn.Execute(command)
		if err != nil {
			slog.Warn("RCON execute failed, will reconnect",
				"address", e.address,
				"error", err,
			)
			_ = e.conn.Close()
			e.conn = nil
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
