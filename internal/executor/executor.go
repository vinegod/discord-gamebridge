// Package executor provides the Executor interface and concrete implementations
// for sending commands to game servers via tmux, RCON, or local scripts.
package executor

import "context"

// Executor sends a command to a target and returns its output.
// Output is an empty string for fire-and-forget transports (tmux).
// Implementations must be safe for concurrent use.
//
// The optional args are appended as positional arguments — used by
// ScriptExecutor to pass dynamic slash command values to the script.
// Tmux and RCON executors ignore args entirely.
type Executor interface {
	Send(ctx context.Context, command string, args ...string) (string, error)
}

// LifecycleExecutor is implemented by executors that hold resources
// (e.g. a persistent TCP connection) that must be released on shutdown.
type LifecycleExecutor interface {
	Executor
	Close() error
}

// HealthChecker is optionally implemented by executors that can report
// whether the target server is currently reachable. Used by the scheduler
// to skip jobs when skip_if_down is true.
type HealthChecker interface {
	Healthy(ctx context.Context) bool
}
