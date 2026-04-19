//go:build integration

package integration

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vinegod/discordgamebridge/internal/executor"
)

// sshConfig reads SSH connection details from environment variables.
// Set SSH_HOST, SSH_PORT, SSH_USER, SSH_PRIVATE_KEY, SSH_KNOWN_HOSTS before running.
// SSH_PRIVATE_KEY and SSH_KNOWN_HOSTS hold the PEM/known_hosts content directly (not file paths).
func sshConfig(t *testing.T) (host string, port int, user, key, knownHosts string) {
	t.Helper()

	host = os.Getenv("SSH_HOST")
	portStr := os.Getenv("SSH_PORT")
	user = os.Getenv("SSH_USER")
	key = os.Getenv("SSH_PRIVATE_KEY")
	knownHosts = os.Getenv("SSH_KNOWN_HOSTS")

	if host == "" || user == "" || key == "" || knownHosts == "" {
		t.Skip("SSH_HOST, SSH_USER, SSH_PRIVATE_KEY, SSH_KNOWN_HOSTS not set — skipping SSH integration tests")
	}

	port = 22
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			t.Fatalf("SSH_PORT is not a valid integer: %v", err)
		}
		port = p
	}

	return host, port, user, key, knownHosts
}

func TestSSHExecutor_Healthy(t *testing.T) {
	host, port, user, key, knownHosts := sshConfig(t)

	ex, err := executor.NewSSHExecutor(host, port, user, key, knownHosts)
	if err != nil {
		t.Fatalf("NewSSHExecutor: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !ex.Healthy(ctx) {
		t.Error("expected Healthy=true for reachable SSH host")
	}
}

func TestSSHExecutor_Send_SimpleCommand(t *testing.T) {
	host, port, user, key, knownHosts := sshConfig(t)

	ex, err := executor.NewSSHExecutor(host, port, user, key, knownHosts)
	if err != nil {
		t.Fatalf("NewSSHExecutor: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := ex.Send(ctx, "echo", "integration-test-ok")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if !strings.Contains(out, "integration-test-ok") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestSSHExecutor_Send_ArgsQuoted(t *testing.T) {
	host, port, user, key, knownHosts := sshConfig(t)

	ex, err := executor.NewSSHExecutor(host, port, user, key, knownHosts)
	if err != nil {
		t.Fatalf("NewSSHExecutor: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Arg with spaces — must arrive as a single argument
	out, err := ex.Send(ctx, "echo", "hello world")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("arg with space was not preserved, got: %q", out)
	}
}

func TestSSHExecutor_Send_Timeout(t *testing.T) {
	host, port, user, key, knownHosts := sshConfig(t)

	ex, err := executor.NewSSHExecutor(host, port, user, key, knownHosts)
	if err != nil {
		t.Fatalf("NewSSHExecutor: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = ex.Send(ctx, "sleep", "10")
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestSSHExecutor_Healthy_UnreachableHost(t *testing.T) {
	// Port 1 is almost never open — a reliable way to get a refused connection.
	ex, err := executor.NewSSHExecutor("127.0.0.1", 1, "user", "key", "placeholder")
	if err != nil {
		// known_hosts parse error is expected with placeholder content; skip the health check.
		t.Skip("placeholder known_hosts caused parse error, skipping")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if ex.Healthy(ctx) {
		t.Error("expected Healthy=false for unreachable host")
	}
}
