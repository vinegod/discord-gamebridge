package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunScript safely verifies and executes a local shell script within the allowed directory bounds
func RunScript(ctx context.Context, scriptPath string, allowedDir string, args []string) (string, error) {
	// Resolve the Allowed Directory to an absolute path
	absAllowedDir, err := filepath.Abs(filepath.Clean(allowedDir))
	if err != nil {
		return "", fmt.Errorf("server misconfiguration: invalid allowed_script_dir")
	}

	// Resolve the requested Script Path to an absolute path
	// This automatically crushes directory traversal attempts like "../../../bin/rm"
	absScriptPath, err := filepath.Abs(filepath.Clean(scriptPath))
	if err != nil {
		return "", fmt.Errorf("invalid script path format")
	}

	// Ensure the resolved script path strictly starts with the allowed directory path
	if !strings.HasPrefix(absScriptPath, absAllowedDir+string(filepath.Separator)) {
		return "", fmt.Errorf("SECURITY VIOLATION: Script '%s' attempts to escape the allowed directory", scriptPath)
	}

	info, err := os.Stat(absScriptPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("script does not exist: %s", absScriptPath)
	}

	if info.IsDir() {
		return "", fmt.Errorf("target is a directory, not an executable script")
	}

	if info.Mode()&0111 == 0 {
		return "", fmt.Errorf("script is not executable: %s (run chmod +x)", absScriptPath)
	}

	cmd := exec.CommandContext(ctx, absScriptPath, args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("script timed out: %w", ctx.Err())
	}
	if err != nil {
		return string(output), fmt.Errorf("execution failed: %w", err)
	}

	return string(output), nil
}
