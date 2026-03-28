// Package executor provides the Executor interface and concrete implementations
// for sending commands to game servers via tmux, RCON, or local scripts.
package executor

import "context"

// Executor sends a command to a target and returns its output.
// Output is an empty string for fire-and-forget transports (tmux).
// Implementations must be safe for concurrent use.
type Executor interface {
	Send(ctx context.Context, command string) (string, error)
}

// LifecycleExecutor is implemented by executors that hold resources
// (e.g. a persistent TCP connection) that must be released on shutdown.
type LifecycleExecutor interface {
	Executor
	Close() error
}
