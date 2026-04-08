package server

import (
	"regexp"
	"strings"
	"testing"

	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/discord"
)

// captureSender records every message passed to Send().
// It satisfies discord.MessageSender.
type captureSender struct {
	messages []discord.Message
}

func (c *captureSender) Send(msg discord.Message) {
	c.messages = append(c.messages, msg)
}

func (c *captureSender) count() int             { return len(c.messages) }
func (c *captureSender) first() discord.Message { return c.messages[0] }

// mustCompile panics only if the regex is genuinely invalid — test configs
// should never use bad patterns.
func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

// terrariaConfig returns a ServerConfig with the real Terraria regex patterns
// used in config_example.yaml. Tests that use different regexes build their
// own config rather than modifying this one.
func terrariaConfig() *config.ServerConfig {
	return &config.ServerConfig{
		CompiledChat:    mustCompile(`^<(?P<player>[^>]+)> (?P<message>.*)$`),
		CompiledJoin:    mustCompile(`^(?P<player>[^\s]+) has joined\.$`),
		CompiledLeave:   mustCompile(`^(?P<player>[^\s]+) has left\.$`),
		CompiledConsole: mustCompile(`^(Terraria Server v|Listening on port|World saved).*`),
		CompiledEvents: mustCompile(
			`.*(has awoken!|have been defeated!|is approaching!|is rising\.\.\.|falling from the sky!)$`,
		),
		CompiledIgnore: mustCompile(`^<Server> .*$`),
	}
}

// ── processLogLine: chat ──────────────────────────────────────────────────────

func TestProcessLogLine_Chat_SendsPlayerMessageWithPlayerUsername(t *testing.T) {
	sender := &captureSender{}
	processLogLine("<Alice> hello everyone", terrariaConfig(), sender)

	if sender.count() != 1 {
		t.Fatalf("expected 1 message, got %d", sender.count())
	}
	msg := sender.first()
	if msg.Username != "Alice" {
		t.Errorf("username: expected 'Alice', got %q", msg.Username)
	}
	if msg.Content != "hello everyone" {
		t.Errorf("content: expected 'hello everyone', got %q", msg.Content)
	}
}

func TestProcessLogLine_Chat_PlayerNameWithSpaces(t *testing.T) {
	sender := &captureSender{}
	processLogLine("<Player One> nice game", terrariaConfig(), sender)

	if sender.count() != 1 {
		t.Fatalf("expected 1 message, got %d", sender.count())
	}
	if sender.first().Username != "Player One" {
		t.Errorf("expected 'Player One', got %q", sender.first().Username)
	}
}

// ── processLogLine: join/leave ────────────────────────────────────────────────

func TestProcessLogLine_Join_SendsJoinMessageWithServerUsername(t *testing.T) {
	sender := &captureSender{}
	processLogLine("Alice has joined.", terrariaConfig(), sender)

	if sender.count() != 1 {
		t.Fatalf("expected 1 message, got %d", sender.count())
	}
	msg := sender.first()
	if msg.Username != "Server" {
		t.Errorf("join message username: expected 'Server', got %q", msg.Username)
	}
	if !strings.Contains(msg.Content, "Alice") {
		t.Errorf("join message should contain player name, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "🟢") {
		t.Errorf("join message should contain green circle emoji, got %q", msg.Content)
	}
}

func TestProcessLogLine_Leave_SendsLeaveMessageWithServerUsername(t *testing.T) {
	sender := &captureSender{}
	processLogLine("Bob has left.", terrariaConfig(), sender)

	if sender.count() != 1 {
		t.Fatalf("expected 1 message, got %d", sender.count())
	}
	msg := sender.first()
	if msg.Username != "Server" {
		t.Errorf("leave message username: expected 'Server', got %q", msg.Username)
	}
	if !strings.Contains(msg.Content, "Bob") {
		t.Errorf("leave message should contain player name, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "🔴") {
		t.Errorf("leave message should contain red circle emoji, got %q", msg.Content)
	}
}

// ── processLogLine: console and events ───────────────────────────────────────

func TestProcessLogLine_Console_SendsWithSystemUsername(t *testing.T) {
	sender := &captureSender{}
	processLogLine("World saved.", terrariaConfig(), sender)

	if sender.count() != 1 {
		t.Fatalf("expected 1 message, got %d", sender.count())
	}
	if sender.first().Username != discord.SystemUsername {
		t.Errorf("console message username: expected %q, got %q",
			discord.SystemUsername, sender.first().Username)
	}
}

func TestProcessLogLine_Events_SendsWithSystemUsername(t *testing.T) {
	sender := &captureSender{}
	processLogLine("Eater of Worlds has awoken!", terrariaConfig(), sender)

	if sender.count() != 1 {
		t.Fatalf("expected 1 message, got %d", sender.count())
	}
	if sender.first().Username != discord.SystemUsername {
		t.Errorf("event message username: expected %q, got %q",
			discord.SystemUsername, sender.first().Username)
	}
	if sender.first().Content != "Eater of Worlds has awoken!" {
		t.Errorf("event content should be the raw line, got %q", sender.first().Content)
	}
}

// ── processLogLine: ignore filter ────────────────────────────────────────────

func TestProcessLogLine_IgnoredLine_NothingForwarded(t *testing.T) {
	// "<Server> ..." matches the ignore regex — the bot sends messages to the
	// game as "say [Discord] User: msg", which Terraria echoes as <Server>.
	// Without the ignore filter, this would create an echo loop.
	sender := &captureSender{}
	processLogLine("<Server> [Discord] Alice: hello", terrariaConfig(), sender)

	if sender.count() != 0 {
		t.Errorf("ignored line should not produce any message, got %d", sender.count())
	}
}

func TestProcessLogLine_IgnorePatternTakesPriorityOverChat(t *testing.T) {
	// Even though "<Server> ..." would match the chat regex,
	// the ignore check must run first and suppress it.
	sender := &captureSender{}
	processLogLine("<Server> this matches chat but should be ignored", terrariaConfig(), sender)

	if sender.count() != 0 {
		t.Errorf(
			"ignore pattern should suppress matching even when chat regex also matches, got %d messages",
			sender.count(),
		)
	}
}

// ── processLogLine: unmatched lines ──────────────────────────────────────────

func TestProcessLogLine_UnmatchedLine_NothingForwarded(t *testing.T) {
	// A line that matches no regex should be silently dropped.
	// This is intentional — unmatched noise from the server shouldn't reach Discord.
	sender := &captureSender{}
	processLogLine("some internal server noise 12345", terrariaConfig(), sender)

	if sender.count() != 0 {
		t.Errorf("unmatched line should be silently dropped, got %d messages", sender.count())
	}
}

func TestProcessLogLine_EmptyLine_NothingForwarded(t *testing.T) {
	sender := &captureSender{}
	processLogLine("", terrariaConfig(), sender)

	if sender.count() != 0 {
		t.Errorf("empty line should produce no messages, got %d", sender.count())
	}
}

func TestProcessLogLine_WhitespaceOnlyLine_NothingForwarded(t *testing.T) {
	sender := &captureSender{}
	processLogLine("   \t  ", terrariaConfig(), sender)

	if sender.count() != 0 {
		t.Errorf("whitespace-only line should produce no messages, got %d", sender.count())
	}
}

// ── processLogLine: nil regex fields ─────────────────────────────────────────

func TestProcessLogLine_NilChatRegex_DoesNotPanic(t *testing.T) {
	cfg := &config.ServerConfig{
		CompiledChat: nil,
		// All others nil too — nothing should match, nothing should panic.
	}
	sender := &captureSender{}
	// Must not panic.
	processLogLine("<Alice> hello", cfg, sender)
}

func TestProcessLogLine_NilIgnoreRegex_StillProcessesOtherRegexes(t *testing.T) {
	cfg := &config.ServerConfig{
		CompiledIgnore: nil, // explicitly no ignore filter
		CompiledJoin:   mustCompile(`^(?P<player>[^\s]+) has joined\.$`),
	}
	sender := &captureSender{}
	processLogLine("Alice has joined.", cfg, sender)

	if sender.count() != 1 {
		t.Errorf("nil ignore regex should not prevent other regexes from matching, got %d messages", sender.count())
	}
}

// ── processLogLine: priority order ───────────────────────────────────────────

func TestProcessLogLine_ChatMatchesFirst_JoinDoesNotAlsoFire(t *testing.T) {
	// If somehow a line matched both chat and join (unlikely with real Terraria
	// patterns, but possible with custom regexes), only one message should be sent.
	cfg := &config.ServerConfig{
		CompiledChat: mustCompile(`^(?P<player>.+) (?P<message>.+)$`), // overly broad
		CompiledJoin: mustCompile(`^(?P<player>[^\s]+) has joined\.$`),
	}
	sender := &captureSender{}
	processLogLine("Alice has joined.", cfg, sender)

	if sender.count() != 1 {
		t.Errorf("only one message should be sent even if multiple regexes could match, got %d", sender.count())
	}
}
