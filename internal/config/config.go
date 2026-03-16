// App configuration file
package config

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"time"

	"github.com/joho/godotenv"
	yaml "gopkg.in/yaml.v3"
)

// Config represents the root of config.yaml
type Config struct {
	Bot      BotConfig       `yaml:"bot"`
	Server   ServerConfig    `yaml:"server"`
	Commands []CommandConfig `yaml:"commands"`
}

// Bot config
type BotConfig struct {
	TokenEnvVar      string `yaml:"token_env_var"`
	LogLevel         string `yaml:"log_level"`
	AllowedScriptDir string `yaml:"allowed_script_dir"`
	Token            string `yaml:"-"`
}

// Server config
type ServerConfig struct {
	TmuxSession string `yaml:"tmux_session"`
	TmuxWindow  int    `yaml:"tmux_window"`
	TmuxPane    int    `yaml:"tmux_pane"`

	DiscordChatChannelID     string `yaml:"discord_chat_channel_id"`
	DiscordWebhookURL        string `yaml:"discord_webhook_url"`
	DiscordConsoleChannelID  string `yaml:"discord_console_channel_id"`
	DiscordConsoleWebhookURL string `yaml:"discord_console_webhook_url"`

	ChatTemplate string        `yaml:"chat_template"`
	ChatTimeout  time.Duration `yaml:"chat_timeout"`
	LogFilePath  string        `yaml:"log_file_path"`
	RegexParsers RegexParsers  `yaml:"regex_parsers"`

	CompiledChat    *regexp.Regexp `yaml:"-"`
	CompiledJoin    *regexp.Regexp `yaml:"-"`
	CompiledLeave   *regexp.Regexp `yaml:"-"`
	CompiledConsole *regexp.Regexp `yaml:"-"`
	CompiledIgnore  *regexp.Regexp `yaml:"-"`
}

// Regex patters for logs
type RegexParsers struct {
	Chat    string `yaml:"chat"`
	Join    string `yaml:"join"`
	Leave   string `yaml:"leave"`
	Console string `yaml:"console"`
	Ignore  string `yaml:"ignore"`
}

type CommandType string

const (
	CommandTypeTmux   CommandType = "tmux"
	CommandTypeScript CommandType = "script"
)

// Command structure config
type CommandConfig struct {
	Name           string           `yaml:"name"`
	Description    string           `yaml:"description"`
	Type           CommandType      `yaml:"type"`
	ScriptPath     string           `yaml:"script_path"`
	Template       string           `yaml:"template"`
	Permissions    PermissionConfig `yaml:"permissions"`
	CommandTimeout time.Duration    `yaml:"script_timeout"`
	StaticArgs     []string         `yaml:"static_args"`
	Arguments      []ArgumentConfig `yaml:"arguments"`
}

// Config for allowed roles and users
type PermissionConfig struct {
	AllowedRoles []string `yaml:"allowed_roles"`
	AllowedUsers []string `yaml:"allowed_users"`
}

type VariableType string

const (
	VariableTypeBool   VariableType = "boolean"
	VariableTypeString VariableType = "string"
)

type ArgumentConfig struct {
	Name        string       `yaml:"name"`
	Type        VariableType `yaml:"type"`
	Description string       `yaml:"description"`
	Required    bool         `yaml:"required"`
}

func (c *Config) applyDefaults() {
	if c.Bot.LogLevel == "" {
		c.Bot.LogLevel = "info"
	}

	if c.Server.ChatTimeout == 0 {
		c.Server.ChatTimeout = 5 * time.Second
	}

	for i := range c.Commands {
		// Prevent instant context cancellation on script/command executions
		if c.Commands[i].CommandTimeout == 0 {
			c.Commands[i].CommandTimeout = 10 * time.Second
		}
	}
}

// Load reads the YAML file, parses the .env, and validates the configuration.
func Load(configPath string) (*Config, error) {
	// Load .env file (ignoring error if it doesn't exist, as env vars might be set in OS)
	_ = godotenv.Load()

	// TODO: Read from repo dir for now, add this option to CLI
	data, err := os.ReadFile(configPath) // #nosec G304
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	cfg.applyDefaults()

	// Load Discord Token
	token := os.Getenv(cfg.Bot.TokenEnvVar)
	if token == "" {
		return nil, fmt.Errorf("critical: Discord token environment variable [%s] is empty", cfg.Bot.TokenEnvVar)
	}
	cfg.Bot.Token = token

	cfg.Server.DiscordWebhookURL = os.Getenv(cfg.Server.DiscordWebhookURL)

	var compileErr error
	cfg.Server.CompiledChat, compileErr = regexp.Compile(cfg.Server.RegexParsers.Chat)
	if compileErr != nil {
		return nil, fmt.Errorf("invalid chat regex: %w", compileErr)
	}

	cfg.Server.CompiledJoin, compileErr = regexp.Compile(cfg.Server.RegexParsers.Join)
	if compileErr != nil {
		return nil, fmt.Errorf("invalid join regex: %w", compileErr)
	}

	cfg.Server.CompiledLeave, compileErr = regexp.Compile(cfg.Server.RegexParsers.Leave)
	if compileErr != nil {
		return nil, fmt.Errorf("invalid leave regex: %w", compileErr)
	}

	cfg.Server.CompiledConsole, compileErr = regexp.Compile(cfg.Server.RegexParsers.Console)
	if compileErr != nil {
		return nil, fmt.Errorf("invalid console regex: %w", compileErr)
	}

	if cfg.Server.RegexParsers.Ignore != "" {
		cfg.Server.CompiledIgnore, compileErr = regexp.Compile(cfg.Server.RegexParsers.Ignore)
		if compileErr != nil {
			slog.Warn("invalid ignore regex", "Error", compileErr)
		}
	}

	return &cfg, nil
}

// Validate checks the loaded configuration for missing or invalid data before the bot starts.
func (c *Config) Validate() error {
	// Validate each enabled c.Server

	slog.Info("Validating Server config")

	if c.Server.ChatTemplate == "" {
		slog.Warn("'chat_template' is empty. Discord-to-Game chat will be DISABLED.")
	}

	if c.Server.DiscordWebhookURL == "" {
		slog.Warn("'discord_webhook_url' is missing. The bot will fallback to standard messages without player avatars.")
	}

	if c.Server.DiscordChatChannelID == "" {
		slog.Warn("'discord_chat_channel_id' is missing. The bot has nowhere to send messages!")
	}

	// Regex Validation
	if c.Server.RegexParsers.Chat == "" {
		slog.Warn("'regex_parsers.chat' is empty. In-game chat will NOT be forwarded to Discord.")
	}
	if c.Server.RegexParsers.Join == "" {
		slog.Warn("'regex_parsers.join' is empty. Join events will be ignored.")
	}
	if c.Server.RegexParsers.Leave == "" {
		slog.Warn("'regex_parsers.leave' is empty. Leave events will be ignored.")
	}

	return nil
}
