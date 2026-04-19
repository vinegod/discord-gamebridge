//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/vinegod/discordgamebridge/internal/executor"
)

// rconConfig reads RCON connection details from environment variables.
// Set RCON_HOST, RCON_PORT, RCON_PASSWORD before running to enable RCON tests.
func rconConfig(t *testing.T) (host string, port int, password string) {
	t.Helper()

	host = os.Getenv("RCON_HOST")
	portStr := os.Getenv("RCON_PORT")
	password = os.Getenv("RCON_PASSWORD")

	if host == "" || portStr == "" || password == "" {
		t.Skip("RCON_HOST, RCON_PORT, RCON_PASSWORD not set — skipping RCON integration tests")
	}

	p, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("RCON_PORT is not a valid integer: %v", err)
	}
	return host, p, password
}

func TestRconExecutor_Connect_And_Healthy(t *testing.T) {
	host, port, password := rconConfig(t)

	ex := executor.NewRconExecutor(host, port, password)
	defer ex.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !ex.Healthy(ctx) {
		t.Error("expected Healthy=true after successful connection")
	}
}

func TestRconExecutor_Send_Command(t *testing.T) {
	host, port, password := rconConfig(t)

	// RCON_COMMAND lets you override the test command for different game servers.
	// Defaults to a safe no-op that most games support.
	command := os.Getenv("RCON_COMMAND")
	if command == "" {
		command = "help"
	}

	ex := executor.NewRconExecutor(host, port, password)
	defer ex.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := ex.Send(ctx, command)
	if err != nil {
		t.Fatalf("Send(%q) failed: %v", command, err)
	}
	t.Logf("server responded: %s", resp)
}

func TestRconExecutor_Timeout(t *testing.T) {
	host, port, password := rconConfig(t)

	ex := executor.NewRconExecutor(host, port, password)
	defer ex.Close() //nolint:errcheck

	// 1ns context will expire before the command executes.
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	_, err := ex.Send(ctx, "help")
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	t.Logf("timeout error (expected): %v", err)
}

func TestRconExecutor_BadCredentials(t *testing.T) {
	host, port, _ := rconConfig(t)

	// Should fail to connect with wrong password.
	ex := executor.NewRconExecutor(host, port, fmt.Sprintf("wrong-password-%d", time.Now().UnixNano()))
	defer ex.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ex.Send(ctx, "help")
	if err == nil {
		t.Error("expected error with wrong password, got nil")
	}
	t.Logf("auth error (expected): %v", err)
}
