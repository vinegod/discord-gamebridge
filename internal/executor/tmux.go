package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// TmuxExecutor sends commands to a specific tmux session/window/pane.
type TmuxExecutor struct {
	Session string
	Window  int
	Pane    int
}

// Send injects command into the configured tmux pane and presses Enter.
func (e *TmuxExecutor) Send(ctx context.Context, command string) (string, error) {
	// TODO: Check G204 issues here and below
	checkCmd := exec.CommandContext(ctx, "tmux", "has-session", "-t", e.Session) // #nosec G204
	if err := checkCmd.Run(); err != nil {
		return "", fmt.Errorf("tmux session %q not found (is the server running?)", e.Session)
	}

	target := fmt.Sprintf("%s:%d.%d", e.Session, e.Window, e.Pane)

	// Step 1: type literal text
	typeCmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "-l", command) // #nosec G204
	if output, err := typeCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to send keys to %q: %w (tmux: %s)",
			target, err, strings.TrimSpace(string(output)))
	}

	// Step 2: press Enter
	enterCmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "C-m") // #nosec G204
	if output, err := enterCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to press Enter on %q: %w (tmux: %s)",
			target, err, strings.TrimSpace(string(output)))
	}

	return "", nil
}
