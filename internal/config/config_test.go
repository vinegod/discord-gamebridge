package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig writes a YAML string to a temp file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	return path
}

// --- applyDefaults ---

func TestApplyDefaults_SetsLogLevel_WhenEmpty(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.Bot.LogLevel != "info" {
		t.Errorf("expected default log level 'info', got %q", cfg.Bot.LogLevel)
	}
}

func TestApplyDefaults_PreservesLogLevel_WhenAlreadySet(t *testing.T) {
	cfg := &Config{Bot: BotConfig{LogLevel: "debug"}}
	cfg.applyDefaults()
	if cfg.Bot.LogLevel != "debug" {
		t.Errorf("expected log level 'debug' preserved, got %q", cfg.Bot.LogLevel)
	}
}

func TestApplyDefaults_SetsChatTimeout_WhenZero(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.Server.ChatTimeout != 5*time.Second {
		t.Errorf("expected default chat timeout 5s, got %v", cfg.Server.ChatTimeout)
	}
}

func TestApplyDefaults_PreservesChatTimeout_WhenAlreadySet(t *testing.T) {
	cfg := &Config{Server: ServerConfig{ChatTimeout: 30 * time.Second}}
	cfg.applyDefaults()
	if cfg.Server.ChatTimeout != 30*time.Second {
		t.Errorf("expected chat timeout 30s preserved, got %v", cfg.Server.ChatTimeout)
	}
}

func TestApplyDefaults_SetsCommandTimeout_ForEachZeroCommand(t *testing.T) {
	cfg := &Config{
		Commands: []CommandConfig{
			{Name: "cmd1"},
			{Name: "cmd2", CommandTimeout: 60 * time.Second},
			{Name: "cmd3"},
		},
	}
	cfg.applyDefaults()

	if cfg.Commands[0].CommandTimeout != 10*time.Second {
		t.Errorf("cmd1: expected default 10s, got %v", cfg.Commands[0].CommandTimeout)
	}
	if cfg.Commands[1].CommandTimeout != 60*time.Second {
		t.Errorf("cmd2: expected 60s preserved, got %v", cfg.Commands[1].CommandTimeout)
	}
	if cfg.Commands[2].CommandTimeout != 10*time.Second {
		t.Errorf("cmd3: expected default 10s, got %v", cfg.Commands[2].CommandTimeout)
	}
}

// --- Validate ---
// Validate currently returns nil regardless of config state (it only logs warnings).
// These tests document existing behaviour and will catch regressions if the
// function is later changed to return errors for critical missing fields.

func TestValidate_ValidConfig_ReturnsNil(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			ChatTemplate:         "say {{.user}}: {{.message}}",
			ChatExecutor:         "tmux",
			DiscordWebhookURL:    "https://discord.com/api/webhooks/x",
			DiscordChatChannelID: "123456789012345678", // gitleaks:allow
			RegexParsers: RegexParsers{
				Chat:  `^<(?P<player>[^>]+)> (?P<message>.*)$`,
				Join:  `^(?P<player>\S+) has joined\.$`,
				Leave: `^(?P<player>\S+) has left\.$`,
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config should not return an error, got: %v", err)
	}
}

func TestValidate_MissingFields_DoesNotPanic(t *testing.T) {
	// An empty config must not panic — all missing fields produce warnings only.
	cfg := &Config{}
	if err := cfg.Validate(); err != nil {
		// Currently returns nil; if this changes to return errors, update the test.
		t.Logf("Validate now returns errors for empty config: %v", err)
	}
}

// --- Load ---

const validConfigYAML = `
bot:
  token_env_var: "TEST_BOT_TOKEN"
  log_level: "info"
  allowed_script_dir: "/tmp/scripts"

server:
  tmux_session: "terraria"
  tmux_window: 1
  tmux_pane: 0
  discord_chat_channel_id: "123456789012345678" # gitleaks:allow
  discord_webhook_env: "TEST_WEBHOOK_URL"
  log_file_path: "/tmp/server.log"
  chat_template: "say {{.user}}: {{.message}}"
  regex_parsers:
    chat:    '^<(?P<player>[^>]+)> (?P<message>.*)$'
    join:    '^(?P<player>\S+) has joined\.$'
    leave:   '^(?P<player>\S+) has left\.$'
    console: '^(Terraria Server|Listening on port).*'

commands: []
`

func TestLoad_ValidConfig_ParsedCorrectly(t *testing.T) {
	path := writeConfig(t, validConfigYAML)
	t.Setenv("TEST_BOT_TOKEN", "my-secret-token")
	t.Setenv("TEST_WEBHOOK_URL", "https://discord.com/api/webhooks/test")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error loading valid config: %v", err)
	}

	if cfg.Bot.Token != "my-secret-token" {
		t.Errorf("expected token from env, got %q", cfg.Bot.Token)
	}
	if cfg.Server.DiscordChatChannelID != "123456789012345678" {
		t.Errorf("expected channel ID, got %q", cfg.Server.DiscordChatChannelID)
	}
}

func TestLoad_TokenLoadedFromEnv(t *testing.T) {
	path := writeConfig(t, validConfigYAML)
	t.Setenv("TEST_BOT_TOKEN", "loaded-from-env")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Bot.Token != "loaded-from-env" {
		t.Errorf("expected token 'loaded-from-env', got %q", cfg.Bot.Token)
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	path := writeConfig(t, validConfigYAML)
	t.Setenv("TEST_BOT_TOKEN", "x")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// chat_timeout not set in YAML — default must be applied.
	if cfg.Server.ChatTimeout != 5*time.Second {
		t.Errorf("expected default chat timeout 5s, got %v", cfg.Server.ChatTimeout)
	}
}

func TestLoad_RegexesCompiled(t *testing.T) {
	path := writeConfig(t, validConfigYAML)
	t.Setenv("TEST_BOT_TOKEN", "x")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.CompiledChat == nil {
		t.Error("expected CompiledChat to be set")
	}
	if cfg.Server.CompiledJoin == nil {
		t.Error("expected CompiledJoin to be set")
	}
	if cfg.Server.CompiledLeave == nil {
		t.Error("expected CompiledLeave to be set")
	}
	if cfg.Server.CompiledConsole == nil {
		t.Error("expected CompiledConsole to be set")
	}
}

func TestLoad_OptionalIgnoreRegex_NotSetWhenEmpty(t *testing.T) {
	path := writeConfig(t, validConfigYAML) // no ignore regex
	t.Setenv("TEST_BOT_TOKEN", "x")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.CompiledIgnore != nil {
		t.Error("CompiledIgnore should be nil when ignore regex is not configured")
	}
}

func TestLoad_OptionalIgnoreRegex_SetWhenProvided(t *testing.T) {
	withIgnore := strings.Replace(
		validConfigYAML,
		"    console: '^(Terraria Server|Listening on port).*'",
		"    console: '^(Terraria Server|Listening on port).*'\n    ignore: '^<Server> .*$'",
		1,
	)
	path := writeConfig(t, withIgnore)
	t.Setenv("TEST_BOT_TOKEN", "x")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.CompiledIgnore == nil {
		t.Error("CompiledIgnore should be set when ignore regex is provided")
	}
}

func TestLoad_MissingFile_ReturnsError(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_MalformedYAML_ReturnsError(t *testing.T) {
	path := writeConfig(t, "bot: [\ninvalid yaml {{{{")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestLoad_MissingToken_ReturnsError(t *testing.T) {
	path := writeConfig(t, validConfigYAML)
	// Ensure the env var is not set.
	t.Setenv("TEST_BOT_TOKEN", "")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing token env var, got nil")
	}
}

func TestLoad_InvalidChatRegex_ReturnsError(t *testing.T) {
	broken := strings.ReplaceAll(validConfigYAML,
		`chat:    '^<(?P<player>[^>]+)> (?P<message>.*)$'`,
		`chat:    '[invalid regex'`,
	)
	path := writeConfig(t, broken)
	t.Setenv("TEST_BOT_TOKEN", "x")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid chat regex, got nil")
	}
	if !strings.Contains(err.Error(), "chat") {
		t.Errorf("error should mention 'chat', got: %v", err)
	}
}

func TestLoad_InvalidJoinRegex_ReturnsError(t *testing.T) {
	broken := strings.ReplaceAll(validConfigYAML,
		`join:    '^(?P<player>\S+) has joined\.$'`,
		`join:    '[invalid'`,
	)
	path := writeConfig(t, broken)
	t.Setenv("TEST_BOT_TOKEN", "x")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid join regex, got nil")
	}
}

func TestLoad_InvalidLeaveRegex_ReturnsError(t *testing.T) {
	broken := strings.ReplaceAll(validConfigYAML,
		`leave:   '^(?P<player>\S+) has left\.$'`,
		`leave:   '[invalid'`,
	)
	path := writeConfig(t, broken)
	t.Setenv("TEST_BOT_TOKEN", "x")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid leave regex, got nil")
	}
}

// --- ParsedChatChannelID ---

func TestParsedChatChannelID_ValidID_Parses(t *testing.T) {
	s := &ServerConfig{DiscordChatChannelID: "123456789012345678"} // gitleaks:allow
	id, err := s.ParsedChatChannelID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.String() != "123456789012345678" {
		t.Errorf("expected ID '123456789012345678', got %q", id.String())
	}
}

func TestParsedChatChannelID_NonNumericString_ReturnsError(t *testing.T) {
	s := &ServerConfig{DiscordChatChannelID: "not-a-snowflake"}
	_, err := s.ParsedChatChannelID()
	if err == nil {
		t.Fatal("expected error for non-numeric channel ID, got nil")
	}
}

func TestParsedChatChannelID_EmptyString_ReturnsError(t *testing.T) {
	s := &ServerConfig{DiscordChatChannelID: ""}
	_, err := s.ParsedChatChannelID()
	if err == nil {
		t.Fatal("expected error for empty channel ID, got nil")
	}
}

func TestParsedChatChannelID_ErrorContainsFieldName(t *testing.T) {
	s := &ServerConfig{DiscordChatChannelID: "bad"}
	_, err := s.ParsedChatChannelID()
	if !strings.Contains(err.Error(), "discord_chat_channel_id") {
		t.Errorf("error should mention the field name, got: %v", err)
	}
}
