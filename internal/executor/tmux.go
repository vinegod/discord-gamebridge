package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// SendCommand targets a specific tmux session, window, and pane, and securely injects a command.
func SendCommand(ctx context.Context, sessionName string, windowIndex, paneIndex int, command string) error {
	// Verify the Session Exists
	// TODO: Check G204 issues here and below
	checkCmd := exec.CommandContext(ctx, "tmux", "has-session", "-t", sessionName) // #nosec G204
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("tmux session '%s' not found (is the server running?)", sessionName)
	}

	// Format the Target Route
	target := fmt.Sprintf("%s:%d.%d", sessionName, windowIndex, paneIndex)

	// Because of issues with tmux we send commands in two steps:
	// Step 1: Type the literal text
	typeCmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "-l", command) // #nosec G204
	output, err := typeCmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(output))
		return fmt.Errorf("failed to type literal text into target '%s': %w (tmux output: %s)", target, err, errMsg)
	}

	// Step 2: Press Enter (C-m)
	enterCmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "C-m") // #nosec G204
	output, err = enterCmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(output))
		return fmt.Errorf("failed to press Enter on target '%s': %w (tmux output: %s)", target, err, errMsg)
	}

	return nil
}
