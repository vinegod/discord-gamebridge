// Package executor provides functions for running system commands and shell scripts securely.
package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ScriptExecutor runs a fixed local shell script.
// StaticArgs are per-command and passed in via Send — see CommandConfig.StaticArgs.
// Implements Executor.
type ScriptExecutor struct {
	AllowedDir string
}

// Send runs the script, passing any args received directly to RunScript.
// The command string is unused — scripts run a fixed path, not a dynamic command.
func (e *ScriptExecutor) Send(ctx context.Context, command string, args ...string) (string, error) {
	return RunScript(ctx, command, e.AllowedDir, args)
}

// RunScript safely verifies and executes a local shell script within the allowed directory bounds.
func RunScript(ctx context.Context, scriptPath, allowedDir string, args []string) (string, error) {
	// Resolve allowed directory: Abs first, then symlinks
	absAllowedDir, err := filepath.Abs(allowedDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve allowed directory: %w", err)
	}
	realAllowedDir, err := filepath.EvalSymlinks(absAllowedDir)
	if err != nil {
		return "", fmt.Errorf("server misconfiguration: invalid allowed_script_dir: %w", err)
	}

	// Build and resolve the script path
	targetPath := filepath.Join(realAllowedDir, scriptPath)
	realScriptPath, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return "", fmt.Errorf("script path resolution failed: %w", err)
	}

	// Boundary check
	relPath, err := filepath.Rel(realAllowedDir, realScriptPath)
	if err != nil {
		return "", fmt.Errorf("could not calculate relative path: %w", err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("security violation: script %q escapes allowed directory", scriptPath)
	}

	// Stat the real, resolved, validated path
	info, err := os.Stat(realScriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("script does not exist: %s", realScriptPath)
		}
		return "", fmt.Errorf("failed to stat script: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("target is a directory, not a script")
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("script is not executable: %s (run chmod +x)", realScriptPath)
	}

	cmd := exec.CommandContext(ctx, realScriptPath, args...) // #nosec G204
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return string(output), fmt.Errorf("script timed out: %w", ctx.Err())
	}
	if err != nil {
		return string(output), fmt.Errorf("execution failed: %w", err)
	}
	return string(output), nil
}
