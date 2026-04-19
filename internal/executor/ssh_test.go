package executor

import (
	"strings"
	"testing"
)

func TestShellJoin_NoArgs(t *testing.T) {
	got := shellJoin("save-all", nil)
	if got != "save-all" {
		t.Errorf("expected %q, got %q", "save-all", got)
	}
}

func TestShellJoin_WithArgs(t *testing.T) {
	got := shellJoin("kick", []string{"player one", "griefing"})
	// Each arg must be single-quoted
	if !strings.Contains(got, "'player one'") {
		t.Errorf("expected single-quoted arg, got %q", got)
	}
	if !strings.Contains(got, "'griefing'") {
		t.Errorf("expected single-quoted arg, got %q", got)
	}
}

func TestShellQuote_EmbeddedSingleQuote(t *testing.T) {
	got := shellQuote("it's fine")
	// Must not break a shell by leaving an unmatched quote
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("expected single-quoted string, got %q", got)
	}
	if strings.Contains(got, "it's fine") {
		// The raw ' inside would break the shell — verify it's escaped
		t.Errorf("embedded single quote was not escaped: %q", got)
	}
}

func TestShellQuote_Plain(t *testing.T) {
	got := shellQuote("hello")
	if got != "'hello'" {
		t.Errorf("expected %q, got %q", "'hello'", got)
	}
}

func TestNewSSHExecutor_MissingKeyFile(t *testing.T) {
	_, err := NewSSHExecutor("host", 22, "user", "/nonexistent/key.pem", "/nonexistent/known_hosts")
	if err == nil {
		t.Error("expected error for missing key file, got nil")
	}
}
