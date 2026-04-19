//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vinegod/discordgamebridge/internal/executor"
)

func scriptFixturesDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("fixtures/scripts")
	if err != nil {
		t.Fatalf("resolve fixtures dir: %v", err)
	}
	return dir
}

func TestScriptExecutor_BasicExecution(t *testing.T) {
	ex := &executor.ScriptExecutor{AllowedDir: scriptFixturesDir(t)}
	out, err := ex.Send(context.Background(), "hello.sh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello from integration test") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestScriptExecutor_ArgsForwarded(t *testing.T) {
	ex := &executor.ScriptExecutor{AllowedDir: scriptFixturesDir(t)}
	out, err := ex.Send(context.Background(), "echo_args.sh", "foo", "bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "foo") || !strings.Contains(out, "bar") {
		t.Errorf("args not forwarded, got: %q", out)
	}
}

func TestScriptExecutor_Timeout(t *testing.T) {
	ex := &executor.ScriptExecutor{AllowedDir: scriptFixturesDir(t)}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := ex.Send(ctx, "slow.sh")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestScriptExecutor_PathTraversalBlocked(t *testing.T) {
	ex := &executor.ScriptExecutor{AllowedDir: scriptFixturesDir(t)}
	_, err := ex.Send(context.Background(), "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected security error for path traversal, got nil")
	}
}

func TestScriptExecutor_NonexistentScript(t *testing.T) {
	ex := &executor.ScriptExecutor{AllowedDir: scriptFixturesDir(t)}
	_, err := ex.Send(context.Background(), "does_not_exist.sh")
	if err == nil {
		t.Fatal("expected error for missing script, got nil")
	}
}

func TestScriptExecutor_NonExecutableScript(t *testing.T) {
	dir := scriptFixturesDir(t)

	// Create a non-executable file
	path := filepath.Join(dir, "noexec.sh")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\necho hi\n"), 0o644); err != nil {
		t.Fatalf("create test file: %v", err)
	}
	t.Cleanup(func() { os.Remove(path) })

	ex := &executor.ScriptExecutor{AllowedDir: dir}
	_, err := ex.Send(context.Background(), "noexec.sh")
	if err == nil {
		t.Fatal("expected error for non-executable script, got nil")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Errorf("unexpected error message: %v", err)
	}
}
