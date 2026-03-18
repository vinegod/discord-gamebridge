// Package executor provides functions for running system commands and shell scripts securely.
package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunScript safely verifies and executes a local shell script within the allowed directory bounds
func RunScript(ctx context.Context, scriptPath, allowedDir string, args []string) (string, error) {
	if strings.Contains(scriptPath, "..") {
		return "", fmt.Errorf("security violation: directory traversal not allowed in script path: %s", scriptPath)
	}

	// Resolve the Allowed Directory to an absolute path, evaluating any symlinks
	realAllowedDir, err := filepath.EvalSymlinks(allowedDir)
	if err != nil {
		return "", fmt.Errorf("server misconfiguration: invalid allowed_script_dir: %w", err)
	}
	realAllowedDir, err = filepath.Abs(realAllowedDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute allowed directory: %w", err)
	}

	// Combine the allowed directory with the provided script name
	targetPath := filepath.Join(realAllowedDir, scriptPath)
	realScriptPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute script path: %w", err)
	}

	info, err := os.Stat(realScriptPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("script does not exist: %s", realScriptPath)
	}

	if info.IsDir() {
		return "", fmt.Errorf("target is a directory, not an executable script")
	}

	// Resolve the requested Script Path, evaluating any symlinks
	// Note: EvalSymlinks will return an error if the file does not exist on disk yet.
	realScriptPath, err = filepath.EvalSymlinks(targetPath)
	if err != nil {
		return "", fmt.Errorf("script path resolution failed: %w", err)
	}

	// Calculate the relative path from the allowed directory to the target script
	relPath, err := filepath.Rel(realAllowedDir, realScriptPath)
	if err != nil {
		return "", fmt.Errorf("could not calculate relative path for script: %w", err)
	}

	// Ensure the relative path does not escape the allowed directory
	if strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || relPath == ".." || relPath == "." {
		slog.Debug("Script violation debug info",
			"Allowed script path", realAllowedDir,
			"Script path", realScriptPath,
			"Relative path", relPath,
		)
		return "", fmt.Errorf("SECURITY VIOLATION: Script '%s' attempts to escape the allowed directory", scriptPath)
	}

	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("script is not executable: %s (run chmod +x)", realScriptPath)
	}

	// TODO: Check G204 issue here
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
