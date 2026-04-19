//go:build integration

package integration

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vinegod/discordgamebridge/internal/executor"
)

const testTmuxSession = "gamebridge-integration-test"

// requireTmux skips the test if tmux is not installed.
func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found in PATH, skipping tmux integration tests")
	}
}

// withTmuxSession creates a detached tmux session for the test and removes it on cleanup.
func withTmuxSession(t *testing.T, session string) {
	t.Helper()
	if err := exec.Command("tmux", "new-session", "-d", "-s", session).Run(); err != nil {
		t.Fatalf("create tmux session %q: %v", session, err)
	}
	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run() //nolint:errcheck
	})
}

// tmuxFirstWindow returns the window and pane indices of the first pane in the session.
// Handles tmux configurations with base-index / pane-base-index other than 0.
func tmuxFirstWindow(t *testing.T, session string) (window, pane int) {
	t.Helper()

	out, err := exec.Command(
		"tmux", "list-panes", "-t", session, "-F", "#{window_index} #{pane_index}",
	).Output()
	if err != nil {
		t.Fatalf("list-panes for session %q: %v", session, err)
	}
	lines := strings.Fields(string(out))
	if len(lines) < 2 {
		t.Fatalf("unexpected list-panes output for session %q: %q", session, out)
	}

	window, err = strconv.Atoi(lines[0])
	if err != nil {
		t.Fatalf("parse window index %q: %v", lines[0], err)
	}
	pane, err = strconv.Atoi(lines[1])
	if err != nil {
		t.Fatalf("parse pane index %q: %v", lines[1], err)
	}
	return window, pane
}

func TestTmuxExecutor_Healthy_ExistingSession(t *testing.T) {
	requireTmux(t)
	withTmuxSession(t, testTmuxSession)

	ex := &executor.TmuxExecutor{Session: testTmuxSession}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !ex.Healthy(ctx) {
		t.Error("expected Healthy=true for existing session")
	}
}

func TestTmuxExecutor_Healthy_MissingSession(t *testing.T) {
	requireTmux(t)

	ex := &executor.TmuxExecutor{Session: "gamebridge-nonexistent-session-xyz"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if ex.Healthy(ctx) {
		t.Error("expected Healthy=false for missing session")
	}
}

func TestTmuxExecutor_Send_ExistingSession(t *testing.T) {
	requireTmux(t)
	withTmuxSession(t, testTmuxSession)

	win, pane := tmuxFirstWindow(t, testTmuxSession)
	ex := &executor.TmuxExecutor{
		Session: testTmuxSession,
		Window:  win,
		Pane:    pane,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Send a no-op command; tmux executors don't return output,
	// so we only verify no error is returned.
	_, err := ex.Send(ctx, "echo integration-test-ok")
	if err != nil {
		t.Errorf("unexpected error sending to tmux session: %v", err)
	}
}

func TestTmuxExecutor_Send_MissingSession(t *testing.T) {
	requireTmux(t)

	ex := &executor.TmuxExecutor{Session: "gamebridge-nonexistent-session-xyz"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ex.Send(ctx, "echo should-fail")
	if err == nil {
		t.Error("expected error when session does not exist")
	}
}
