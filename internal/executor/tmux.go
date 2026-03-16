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
	checkCmd := exec.CommandContext(ctx, "tmux", "has-session", "-t", sessionName)
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("tmux session '%s' not found (is the server running?)", sessionName)
	}

	// Format the Target Route
	target := fmt.Sprintf("%s:%d.%d", sessionName, windowIndex, paneIndex)

	// TODO: Check G204
	// STEP 1: Type the literal text (-l flag)
	typeCmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "-l", command) // #nosec G204
	output, err := typeCmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(output))
		return fmt.Errorf("failed to type literal text into target '%s': %v (tmux output: %s)", target, err, errMsg)
	}

	// STEP 2: Press Enter (C-m)
	enterCmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "C-m") // #nosec G204
	output, err = enterCmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(output))
		return fmt.Errorf("failed to press Enter on target '%s': %v (tmux output: %s)", target, err, errMsg)
	}

	return nil
}
