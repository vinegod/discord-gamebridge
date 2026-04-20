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

// ── shellQuote injection safety ───────────────────────────────────────────────

func TestShellQuote_SemicolonInjection_IsContained(t *testing.T) {
	// A semicolon in an arg must not allow a second command to execute.
	// After quoting, the semicolon must appear inside single quotes.
	got := shellQuote("; rm -rf /")
	if strings.Contains(got, "; rm") && !strings.HasPrefix(got, "'") {
		t.Errorf("semicolon injection not properly quoted: %q", got)
	}
	// The whole thing must be wrapped in single quotes.
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("expected single-quoted result, got %q", got)
	}
}

func TestShellQuote_BacktickInjection_IsContained(t *testing.T) {
	got := shellQuote("`whoami`")
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("backtick injection not properly quoted: %q", got)
	}
}

func TestShellQuote_DollarExpansion_IsContained(t *testing.T) {
	got := shellQuote("$(cat /etc/passwd)")
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("dollar expansion not properly quoted: %q", got)
	}
}

func TestShellJoin_AllArgsQuoted(t *testing.T) {
	// Every argument, including those with metacharacters, must be quoted.
	args := []string{"safe", "with space", "semi;colon", "dollar$var", "back`tick`"}
	got := shellJoin("cmd", args)
	for _, arg := range args {
		quoted := "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
		if !strings.Contains(got, quoted) {
			t.Errorf("arg %q not properly quoted in: %q", arg, got)
		}
	}
}
