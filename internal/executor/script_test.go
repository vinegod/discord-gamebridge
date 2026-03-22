package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeScript creates a shell script in dir and returns its name.
func writeScript(t *testing.T, dir, name, body string, executable bool) {
	t.Helper()
	perm := os.FileMode(0o600)
	if executable {
		perm = 0o750
	}
	content := "#!/bin/sh\n" + body
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), perm); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
}

// --- Happy paths ---

func TestRunScript_ReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "hello.sh", "echo hello", true)

	out, err := RunScript(context.Background(), "hello.sh", dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected 'hello' in output, got %q", out)
	}
}

func TestRunScript_NestedScriptInSubdirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeScript(t, filepath.Join(dir, "sub"), "nested.sh", "echo nested", true)

	out, err := RunScript(context.Background(), "sub/nested.sh", dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "nested") {
		t.Errorf("expected 'nested' in output, got %q", out)
	}
}

func TestRunScript_CapturesStdoutAndStderr(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "combined.sh", "echo stdout\necho stderr >&2", true)

	out, err := RunScript(context.Background(), "combined.sh", dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "stdout") || !strings.Contains(out, "stderr") {
		t.Errorf("expected both stdout and stderr in output, got %q", out)
	}
}

// --- Error: file state ---

func TestRunScript_ScriptDoesNotExist(t *testing.T) {
	dir := t.TempDir()

	_, err := RunScript(context.Background(), "missing.sh", dir, nil)
	if err == nil {
		t.Fatal("expected error for missing script, got nil")
	}
}

func TestRunScript_ScriptIsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "notscript"), 0o750); err != nil {
		t.Fatal(err)
	}

	_, err := RunScript(context.Background(), "notscript", dir, nil)
	if err == nil {
		t.Fatal("expected error when target is a directory, got nil")
	}
}

func TestRunScript_ScriptNotExecutable(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "noexec.sh", "echo hi", false)

	_, err := RunScript(context.Background(), "noexec.sh", dir, nil)
	if err == nil {
		t.Fatal("expected error for non-executable script, got nil")
	}
	if !strings.Contains(err.Error(), "chmod") {
		t.Errorf("expected error to mention chmod, got: %v", err)
	}
}

func TestRunScript_InvalidAllowedDir(t *testing.T) {
	_, err := RunScript(context.Background(), "script.sh", "/this/path/does/not/exist", nil)
	if err == nil {
		t.Fatal("expected error for non-existent allowed dir, got nil")
	}
}

// --- Error: path traversal / security ---

func TestRunScript_PathTraversal_DotDot(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	writeScript(t, outside, "evil.sh", "echo evil", true)

	// Try to reach outside/evil.sh from inside allowed using ../
	rel := filepath.Join("..", filepath.Base(outside), "evil.sh")
	_, err := RunScript(context.Background(), rel, allowed, nil)
	if err == nil {
		t.Fatal("expected security error for path traversal, got nil")
	}
	errStr := strings.ToLower(err.Error())
	if !strings.Contains(errStr, "escapes") && !strings.Contains(errStr, "security") {
		t.Errorf("expected security violation error, got: %v", err)
	}
}

func TestRunScript_PathTraversal_SymlinkEscape(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()

	writeScript(t, outside, "evil.sh", "echo evil", false)

	// Symlink inside allowed pointing to script outside allowed
	link := filepath.Join(allowed, "link.sh")
	if err := os.Symlink(filepath.Join(outside, "evil.sh"), link); err != nil {
		t.Skip("symlinks not supported:", err)
	}

	_, err := RunScript(context.Background(), "link.sh", allowed, nil)
	if err == nil {
		t.Fatal("expected error for symlink escaping allowed dir, got nil")
	}
}

// --- Error: execution ---

func TestRunScript_Timeout_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "sleep.sh", "sleep 10", true)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := RunScript(ctx, "sleep.sh", dir, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// TestRunScript_FailedScript_ReturnsOutputAndError verifies that stderr from a
// failing script is still returned to the caller alongside the error.
func TestRunScript_FailedScript_ReturnsOutputAndError(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "fail_stderr.sh", `echo "failure reason" >&2
exit 1`, true)

	out, err := RunScript(context.Background(), "fail_stderr.sh", dir, nil)
	if err == nil {
		t.Fatal("expected non-zero exit error, got nil")
	}
	if !strings.Contains(out, "failure reason") {
		t.Errorf("stderr should be captured even on failure, got %q", out)
	}
}

// TestRunScript_ScriptIsExactlyAllowedDir checks the boundary where the
// resolved script path equals the allowed directory itself (relPath == ".").
// This should be rejected — a directory is not a script.
func TestRunScript_ScriptIsExactlyAllowedDir_Rejected(t *testing.T) {
	dir := t.TempDir()

	// Pass the directory itself as the script path.
	_, err := RunScript(context.Background(), ".", dir, nil)
	if err == nil {
		t.Fatal("expected error when script path resolves to the allowed directory itself, got nil")
	}
}

// TestRunScript_BoolArg_AppendedAsFlagName verifies that boolean-style
// arguments (--flag-name) are passed correctly as separate argv entries.
func TestRunScript_MultipleArgs_AllPassedCorrectly(t *testing.T) {
	dir := t.TempDir()
	// Print each argument on its own line.
	writeScript(t, dir, "args.sh", `for arg in "$@"; do echo "$arg"; done`, true)

	out, err := RunScript(context.Background(), "args.sh", dir, []string{"first", "second", "third"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, expected := range []string{"first", "second", "third"} {
		if !strings.Contains(out, expected) {
			t.Errorf("expected arg %q in output, got %q", expected, out)
		}
	}
}
