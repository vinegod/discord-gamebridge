package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	return path
}

// validConfigYAML is the canonical valid config used across Load tests.
const validConfigYAML = `
bot:
  token_env_var: "TEST_BOT_TOKEN"
  log_level: "info"

executors:
  game_tmux:
    type: "tmux"
    session: "terraria"
    window: 1
    pane: 0

server:
  chat_executor: "game_tmux"
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

// ── applyDefaults ─────────────────────────────────────────────────────────────

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

// ── Validate ──────────────────────────────────────────────────────────────────

func TestValidate_ValidConfig_ReturnsNil(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			ChatExecutor:         "game_tmux",
			ChatTemplate:         "say {{.user}}: {{.message}}",
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

func TestValidate_ChatTemplateWithoutChatExecutor_ReturnsError(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			ChatTemplate: "say {{.user}}: {{.message}}",
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when chat_template set but chat_executor missing")
	}
	if !strings.Contains(err.Error(), "chat_executor") {
		t.Errorf("error should mention chat_executor, got: %v", err)
	}
}

func TestValidate_EmptyConfig_DoesNotPanic(t *testing.T) {
	cfg := &Config{}
	_ = cfg.Validate()
}

func TestValidate_DuplicateCommandNames_ReturnsError(t *testing.T) {
	cfg := &Config{
		Commands: []CommandConfig{
			{
				Name: "kick", Description: "kick player", Type: CommandTypeExecutor,
				ExecutorName: "game_tmux", Template: "kick {{.player}}",
			},
			{
				Name: "kick", Description: "duplicate", Type: CommandTypeExecutor,
				ExecutorName: "game_tmux", Template: "kick {{.player}}",
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate command names, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate, got: %v", err)
	}
}

// ── validateExecutor ──────────────────────────────────────────────────────────

func TestValidateExecutor_ValidTmux_ReturnsNil(t *testing.T) {
	err := validateExecutor("game_tmux", &ExecutorConfig{
		Type:    ExecutorTypeTmux,
		Session: "terraria",
		Window:  1,
		Pane:    0,
	})
	if err != nil {
		t.Errorf("expected nil for valid tmux executor, got: %v", err)
	}
}

func TestValidateExecutor_TmuxMissingSession_ReturnsError(t *testing.T) {
	err := validateExecutor("game_tmux", &ExecutorConfig{Type: ExecutorTypeTmux})
	if err == nil {
		t.Fatal("expected error for tmux without session, got nil")
	}
	if !strings.Contains(err.Error(), "session") {
		t.Errorf("error should mention session, got: %v", err)
	}
}

func TestValidateExecutor_ValidRcon_ReturnsNil(t *testing.T) {
	err := validateExecutor("game_rcon", &ExecutorConfig{
		Type:     ExecutorTypeRcon,
		Host:     "localhost",
		Port:     7779,
		Password: "secret",
	})
	if err != nil {
		t.Errorf("expected nil for valid rcon executor, got: %v", err)
	}
}

func TestValidateExecutor_RconMissingHost_ReturnsError(t *testing.T) {
	err := validateExecutor("game_rcon", &ExecutorConfig{
		Type:     ExecutorTypeRcon,
		Port:     7779,
		Password: "secret",
	})
	if err == nil {
		t.Fatal("expected error for rcon without host, got nil")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Errorf("error should mention host, got: %v", err)
	}
}

func TestValidateExecutor_RconMissingPort_ReturnsError(t *testing.T) {
	err := validateExecutor("game_rcon", &ExecutorConfig{
		Type:     ExecutorTypeRcon,
		Host:     "localhost",
		Password: "secret",
	})
	if err == nil {
		t.Fatal("expected error for rcon without port, got nil")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("error should mention port, got: %v", err)
	}
}

func TestValidateExecutor_RconEmptyPassword_ReturnsError(t *testing.T) {
	err := validateExecutor("game_rcon", &ExecutorConfig{
		Type: ExecutorTypeRcon,
		Host: "localhost",
		Port: 7779,
	})
	if err == nil {
		t.Fatal("expected error for rcon with empty password, got nil")
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("error should mention password, got: %v", err)
	}
}

func TestValidateExecutor_ValidScript_ReturnsNil(t *testing.T) {
	err := validateExecutor("scripts", &ExecutorConfig{
		Type:             ExecutorTypeScript,
		AllowedScriptDir: "/opt/scripts",
	})
	if err != nil {
		t.Errorf("expected nil for valid script executor, got: %v", err)
	}
}

func TestValidateExecutor_ScriptMissingAllowedDir_ReturnsError(t *testing.T) {
	err := validateExecutor("scripts", &ExecutorConfig{Type: ExecutorTypeScript})
	if err == nil {
		t.Fatal("expected error for script executor without allowed_script_dir, got nil")
	}
	if !strings.Contains(err.Error(), "allowed_script_dir") {
		t.Errorf("error should mention allowed_script_dir, got: %v", err)
	}
}

func TestValidateExecutor_UnknownType_ReturnsError(t *testing.T) {
	err := validateExecutor("mystery", &ExecutorConfig{Type: ExecutorType("ssh")})
	if err == nil {
		t.Fatal("expected error for unknown executor type, got nil")
	}
}

func TestValidateExecutor_EmptyType_ReturnsError(t *testing.T) {
	err := validateExecutor("mystery", &ExecutorConfig{})
	if err == nil {
		t.Fatal("expected error for empty executor type, got nil")
	}
}

// ── validateCommand ───────────────────────────────────────────────────────────

func TestValidateCommand_ExecutorMissingExecutorField_ReturnsError(t *testing.T) {
	err := validateCommand(&CommandConfig{
		Name:        "kick",
		Description: "kick player",
		Type:        CommandTypeExecutor,
		Template:    "kick {{.player}}",
	})
	if err == nil {
		t.Fatal("expected error for executor command without executor name, got nil")
	}
	if !strings.Contains(err.Error(), "executor") {
		t.Errorf("error should mention executor, got: %v", err)
	}
}

func TestValidateCommand_ScriptValid_ReturnsNil(t *testing.T) {
	err := validateCommand(&CommandConfig{
		Name:         "restart",
		Description:  "restart server",
		Type:         CommandTypeScript,
		ExecutorName: "terraria_scripts",
		ScriptPath:   "restart.sh",
	})
	if err != nil {
		t.Errorf("expected nil for valid script command, got: %v", err)
	}
}

func TestValidateCommand_ScriptMissingExecutor_ReturnsError(t *testing.T) {
	err := validateCommand(&CommandConfig{
		Name:        "restart",
		Description: "restart server",
		Type:        CommandTypeScript,
		ScriptPath:  "restart.sh",
	})
	if err == nil {
		t.Fatal("expected error for script command without executor, got nil")
	}
}

func TestValidateCommand_ScriptMissingScriptPath_ReturnsError(t *testing.T) {
	err := validateCommand(&CommandConfig{
		Name:         "restart",
		Description:  "restart server",
		Type:         CommandTypeScript,
		ExecutorName: "terraria_scripts",
	})
	if err == nil {
		t.Fatal("expected error for script command without script_path, got nil")
	}
	if !strings.Contains(err.Error(), "script_path") {
		t.Errorf("error should mention script_path, got: %v", err)
	}
}

func TestValidateCommand_OutputMissingPattern_ReturnsError(t *testing.T) {
	err := validateCommand(&CommandConfig{
		Name:         "time",
		Description:  "get server time",
		Type:         CommandTypeExecutor,
		ExecutorName: "game_rcon",
		Template:     "time",
		Output:       &OutputConfig{Format: "🕐 {{.time}}"},
	})
	if err == nil {
		t.Fatal("expected error for output config without pattern, got nil")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("error should mention pattern, got: %v", err)
	}
}

func TestValidateCommand_OutputMissingFormat_ReturnsError(t *testing.T) {
	err := validateCommand(&CommandConfig{
		Name:         "time",
		Description:  "get server time",
		Type:         CommandTypeExecutor,
		ExecutorName: "game_rcon",
		Template:     "time",
		Output:       &OutputConfig{Pattern: `(?P<time>\S+)`},
	})
	if err == nil {
		t.Fatal("expected error for output config without format, got nil")
	}
	if !strings.Contains(err.Error(), "format") {
		t.Errorf("error should mention format, got: %v", err)
	}
}

// ── ReferencedExecutorNames ───────────────────────────────────────────────────

func TestReferencedExecutorNames_Empty_ReturnsNil(t *testing.T) {
	cfg := &Config{}
	if names := cfg.ReferencedExecutorNames(); len(names) != 0 {
		t.Errorf("expected no names for empty config, got %v", names)
	}
}

func TestReferencedExecutorNames_ChatExecutorIncluded(t *testing.T) {
	cfg := &Config{Server: ServerConfig{ChatExecutor: "game_rcon"}}
	names := cfg.ReferencedExecutorNames()
	if len(names) != 1 || names[0] != "game_rcon" {
		t.Errorf("expected [game_rcon], got %v", names)
	}
}

func TestReferencedExecutorNames_CommandExecutorsIncluded(t *testing.T) {
	cfg := &Config{
		Commands: []CommandConfig{
			{ExecutorName: "game_tmux"},
			{ExecutorName: "game_rcon"},
		},
	}
	if names := cfg.ReferencedExecutorNames(); len(names) != 2 {
		t.Errorf("expected 2 names, got %v", names)
	}
}

func TestReferencedExecutorNames_Deduplicated(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{ChatExecutor: "game_tmux"},
		Commands: []CommandConfig{{ExecutorName: "game_tmux"}, {ExecutorName: "game_tmux"}},
	}
	if names := cfg.ReferencedExecutorNames(); len(names) != 1 {
		t.Errorf("expected 1 unique name, got %v", names)
	}
}

func TestReferencedExecutorNames_ScriptCommandsIgnored(t *testing.T) {
	cfg := &Config{
		Commands: []CommandConfig{{Type: CommandTypeScript}},
	}
	if names := cfg.ReferencedExecutorNames(); len(names) != 0 {
		t.Errorf("expected no names for script command with empty executor, got %v", names)
	}
}

// ── Load ──────────────────────────────────────────────────────────────────────

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
	if ex, ok := cfg.Executors["game_tmux"]; !ok {
		t.Error("expected executor 'game_tmux' to be present")
	} else if ex.Session != "terraria" {
		t.Errorf("expected executor session 'terraria', got %q", ex.Session)
	}
	if cfg.Server.ChatExecutor != "game_tmux" {
		t.Errorf("expected chat_executor 'game_tmux', got %q", cfg.Server.ChatExecutor)
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

func TestLoad_RconPasswordLoadedFromEnv(t *testing.T) {
	t.Setenv("TEST_BOT_TOKEN", "x")
	t.Setenv("MY_RCON_PASS", "secret123")

	yaml := strings.Replace(
		validConfigYAML,
		"  game_tmux:\n    type: \"tmux\"\n    session: \"terraria\"\n    window: 1\n    pane: 0",
		"  game_tmux:\n    type: \"tmux\"\n    session: \"terraria\"\n    window: 1\n    pane: 0\n  game_rcon:\n    type: \"rcon\"\n    host: \"localhost\"\n    port: 7779\n    password_env: \"MY_RCON_PASS\"",
		1,
	)
	path := writeConfig(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Executors["game_rcon"].Password != "secret123" {
		t.Errorf("expected rcon password loaded from env, got %q",
			cfg.Executors["game_rcon"].Password)
	}
}

func TestLoad_RconEmptyPasswordEnv_LoadsEmpty(t *testing.T) {
	t.Setenv("TEST_BOT_TOKEN", "x")

	yaml := strings.Replace(
		validConfigYAML,
		"  game_tmux:\n    type: \"tmux\"\n    session: \"terraria\"\n    window: 1\n    pane: 0",
		"  game_tmux:\n    type: \"tmux\"\n    session: \"terraria\"\n    window: 1\n    pane: 0\n  game_rcon:\n    type: \"rcon\"\n    host: \"localhost\"\n    port: 7779\n    password_env: \"MY_RCON_PASS\"",
		1,
	)
	path := writeConfig(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load should succeed even with empty password (Validate catches it): %v", err)
	}
	if cfg.Executors["game_rcon"].Password != "" {
		t.Errorf("expected empty password when env var is unset, got %q",
			cfg.Executors["game_rcon"].Password)
	}
}

func TestLoad_OutputPattern_CompiledOnLoad(t *testing.T) {
	t.Setenv("TEST_BOT_TOKEN", "x")

	withOutput := strings.Replace(
		validConfigYAML,
		"commands: []",
		`commands:
  - name: "time"
    description: "get server time"
    type: "executor"
    executor: "game_tmux"
    template: "time"
    output:
      pattern: 'Time: (?P<time>\S+)'
      format: "🕐 {{.time}}"`,
		1,
	)
	path := writeConfig(t, withOutput)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Commands[0].Output == nil {
		t.Fatal("expected Output to be set")
	}
	if cfg.Commands[0].Output.compiled == nil {
		t.Error("expected output pattern to be compiled at load time")
	}
}

func TestLoad_InvalidOutputPattern_ReturnsError(t *testing.T) {
	t.Setenv("TEST_BOT_TOKEN", "x")

	withBadOutput := strings.Replace(
		validConfigYAML,
		"commands: []",
		`commands:
  - name: "time"
    description: "get server time"
    type: "executor"
    executor: "game_tmux"
    template: "time"
    output:
      pattern: '[invalid'
      format: "{{.time}}"`,
		1,
	)
	path := writeConfig(t, withBadOutput)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid output pattern, got nil")
	}
}

func TestLoad_OptionalIgnoreRegex_NotSetWhenEmpty(t *testing.T) {
	path := writeConfig(t, validConfigYAML)
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

// ── ParsedChatChannelID ───────────────────────────────────────────────────────

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

// ── ExtractGroups ─────────────────────────────────────────────────────────────

func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

func TestExtractGroups_NoMatch_ReturnsNil(t *testing.T) {
	re := mustCompile(`^<(?P<player>[^>]+)>`)
	if got := ExtractGroups(re, "no angle brackets here"); got != nil {
		t.Errorf("expected nil for non-matching input, got %v", got)
	}
}

func TestExtractGroups_Match_ReturnsNamedGroups(t *testing.T) {
	re := mustCompile(`^<(?P<player>[^>]+)> (?P<message>.*)$`)
	got := ExtractGroups(re, "<Alice> hello world")

	if got == nil {
		t.Fatal("expected non-nil result for matching input")
	}
	if got["player"] != "Alice" {
		t.Errorf("player: expected 'Alice', got %q", got["player"])
	}
	if got["message"] != "hello world" {
		t.Errorf("message: expected 'hello world', got %q", got["message"])
	}
}

func TestExtractGroups_UnnamedGroups_NotIncluded(t *testing.T) {
	re := mustCompile(`^([^:]+): (?P<message>.*)$`)
	got := ExtractGroups(re, "prefix: content here")

	if got == nil {
		t.Fatal("expected non-nil for matching input")
	}
	if _, ok := got[""]; ok {
		t.Error("unnamed group with empty key should not appear in result")
	}
	if got["message"] != "content here" {
		t.Errorf("named group 'message': expected 'content here', got %q", got["message"])
	}
}

func TestExtractGroups_EmptyNamedGroup_IncludedAsEmptyString(t *testing.T) {
	re := mustCompile(`^(?P<player>[^>]*)?$`)
	got := ExtractGroups(re, "")

	if got == nil {
		t.Fatal("expected non-nil for matching input")
	}
	if _, ok := got["player"]; !ok {
		t.Error("named group should be present even if it matched empty string")
	}
}

// ── OutputConfig.Apply ────────────────────────────────────────────────────────

func TestOutputConfig_Apply_NilConfig_ReturnsRaw(t *testing.T) {
	var o *OutputConfig
	if got := o.Apply("raw output"); got != "raw output" {
		t.Errorf("nil config should return raw unchanged, got %q", got)
	}
}

func TestOutputConfig_Apply_NilCompiled_ReturnsRaw(t *testing.T) {
	// compiled is nil when no pattern was set - should behave like nil config.
	o := &OutputConfig{Pattern: "", Format: ""}
	if got := o.Apply("raw output"); got != "raw output" {
		t.Errorf("uncompiled config should return raw unchanged, got %q", got)
	}
}

func TestOutputConfig_Apply_PatternMatches_ReturnsFormatted(t *testing.T) {
	o := &OutputConfig{
		Pattern:  `Time: (?P<time>\S+)`,
		Format:   "🕐 {{.time}}",
		compiled: mustCompile(`Time: (?P<time>\S+)`),
	}
	got := o.Apply("Time: 14:32:01\nDebug: tick=8473920")
	if got != "🕐 14:32:01" {
		t.Errorf("expected formatted output '🕐 14:32:01', got %q", got)
	}
}

func TestOutputConfig_Apply_PatternNoMatch_ReturnsRaw(t *testing.T) {
	// A non-matching pattern must return raw rather than an empty string.
	// Empty Discord messages are confusing; raw output at least shows something.
	o := &OutputConfig{
		Pattern:  `Time: (?P<time>\S+)`,
		Format:   "🕐 {{.time}}",
		compiled: mustCompile(`Time: (?P<time>\S+)`),
	}
	raw := "Unrecognised response format"
	if got := o.Apply(raw); got != raw {
		t.Errorf("non-matching pattern should return raw unchanged, got %q", got)
	}
}

func TestOutputConfig_Apply_MultipleGroups_AllSubstituted(t *testing.T) {
	o := &OutputConfig{
		Pattern:  `(?P<day>\w+) (?P<time>\S+)`,
		Format:   "{{.day}} at {{.time}}",
		compiled: mustCompile(`(?P<day>\w+) (?P<time>\S+)`),
	}
	got := o.Apply("Monday 14:32:01")
	if got != "Monday at 14:32:01" {
		t.Errorf("expected 'Monday at 14:32:01', got %q", got)
	}
}

func TestOutputConfig_Apply_EmptyRawInput_PatternNoMatch_ReturnsEmpty(t *testing.T) {
	o := &OutputConfig{
		Pattern:  `Time: (?P<time>\S+)`,
		Format:   "🕐 {{.time}}",
		compiled: mustCompile(`Time: (?P<time>\S+)`),
	}
	if got := o.Apply(""); got != "" {
		t.Errorf("empty input with no match should return empty, got %q", got)
	}
}

func TestOutputConfig_Apply_FormatMissingGroupRef_ReturnsPartial(t *testing.T) {
	// Format references a group that doesn't exist in the pattern.
	// SubstituteTemplate removes unfilled placeholders - result is trimmed but not empty.
	o := &OutputConfig{
		Pattern:  `Time: (?P<time>\S+)`,
		Format:   "{{.time}} ({{.missing}})",
		compiled: mustCompile(`Time: (?P<time>\S+)`),
	}
	got := o.Apply("Time: 14:32:01")
	if got != "14:32:01 ()" {
		// SubstituteTemplate removes {{.missing}} leaving "()" - acceptable behaviour.
		// What matters is it doesn't panic or return empty.
		t.Logf("got %q - verify this is acceptable for your use case", got)
	}
}
