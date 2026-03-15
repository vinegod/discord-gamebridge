package config

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config represents the root of config.yaml
type Config struct {
	Bot      BotConfig                `yaml:"bot"`
	Bridges  map[string]*BridgeConfig `yaml:"bridges"`
	Commands []CommandConfig          `yaml:"commands"`
}

type BotConfig struct {
	TokenEnvVar      string `yaml:"token_env_var"`
	LogLevel         string `yaml:"log_level"`
	AllowedScriptDir string `yaml:"allowed_script_dir"`
	Token            string `yaml:"-"`
}

type BridgeConfig struct {
	Enabled                 bool           `yaml:"enabled"`
	TmuxSession             string         `yaml:"tmux_session"`
	TmuxWindow              int            `yaml:"tmux_window"`
	TmuxPane                int            `yaml:"tmux_pane"`
	DiscordChatChannelID    string         `yaml:"discord_chat_channel_id"`
	DiscordConsoleChannelID string         `yaml:"discord_console_channel_id"`
	DiscordWebhookURL       string         `yaml:"discord_webhook_url"`
	ChatTemplate            string         `yaml:"chat_template"`
	ChatTimeout             time.Duration  `yaml:"chat_timeout"`
	IgnoreChatNames         []string       `yaml:"ignore_chat_names"`
	LogFilePath             string         `yaml:"log_file_path"`
	RegexParsers            RegexParsers   `yaml:"regex_parsers"`
	CompiledChat            *regexp.Regexp `yaml:"-"`
	CompiledJoin            *regexp.Regexp `yaml:"-"`
	CompiledLeave           *regexp.Regexp `yaml:"-"`
	CompiledConsole         *regexp.Regexp `yaml:"-"`
	CompiledIgnore          *regexp.Regexp `yaml:"-"`
}

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

type CommandConfig struct {
	Name           string           `yaml:"name"`
	Description    string           `yaml:"description"`
	Type           CommandType      `yaml:"type"`
	TargetBridge   string           `yaml:"target_bridge"`
	ScriptPath     string           `yaml:"script_path"`
	Template       string           `yaml:"template"`
	Permissions    PermissionConfig `yaml:"permissions"`
	CommandTimeout time.Duration    `yaml:"script_timeout"`
	StaticArgs     []string         `yaml:"static_args"`
	Arguments      []ArgumentConfig `yaml:"arguments"`
}

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

	for _, bridge := range c.Bridges {
		// Prevent instant context cancellation on tmux calls
		if bridge.ChatTimeout == 0 {
			bridge.ChatTimeout = 5 * time.Second
		}
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

	data, err := os.ReadFile(configPath)
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
		return nil, fmt.Errorf("critical: Discord token environment variable '%s' is empty", cfg.Bot.TokenEnvVar)
	}
	cfg.Bot.Token = token

	// Pre-compile regular expressions
	for name, bridge := range cfg.Bridges {
		if !bridge.Enabled {
			continue
		}

		bridge.DiscordWebhookURL = os.Getenv(bridge.DiscordWebhookURL)

		var compileErr error
		bridge.CompiledChat, compileErr = regexp.Compile(bridge.RegexParsers.Chat)
		if compileErr != nil {
			return nil, fmt.Errorf("invalid chat regex in bridge '%s': %w", name, compileErr)
		}

		bridge.CompiledJoin, compileErr = regexp.Compile(bridge.RegexParsers.Join)
		if compileErr != nil {
			return nil, fmt.Errorf("invalid join regex in bridge '%s': %w", name, compileErr)
		}

		bridge.CompiledLeave, compileErr = regexp.Compile(bridge.RegexParsers.Leave)
		if compileErr != nil {
			return nil, fmt.Errorf("invalid leave regex in bridge '%s': %w", name, compileErr)
		}

		bridge.CompiledConsole, compileErr = regexp.Compile(bridge.RegexParsers.Console)
		if compileErr != nil {
			return nil, fmt.Errorf("invalid console regex in bridge '%s': %w", name, compileErr)
		}

		if bridge.RegexParsers.Ignore != "" {
			bridge.CompiledIgnore, compileErr = regexp.Compile(bridge.RegexParsers.Ignore)
			if compileErr != nil {
				slog.Warn("invalid ignore regex in bridge", "Name", name, "Error", compileErr)
			}
		}
	}

	return &cfg, nil
}

// Validate checks the loaded configuration for missing or invalid data before the bot starts.
func (c *Config) Validate() error {
	// Validate each enabled bridge
	for bridgeName, bridge := range c.Bridges {
		if !bridge.Enabled {
			continue
		}

		slog.Info("Validating bridge: ", "bridge", bridgeName)

		if bridge.ChatTemplate == "" {
			slog.Warn("'chat_template' is empty. Discord-to-Game chat will be DISABLED.", "bridge", bridgeName)
		}

		if bridge.DiscordWebhookURL == "" {
			slog.Warn("'discord_webhook_url' is missing. The bot will fallback to standard messages without player avatars.", "bridge", bridgeName)
		}

		if bridge.DiscordChatChannelID == "" {
			slog.Warn("'discord_chat_channel_id' is missing. The bot has nowhere to send messages!", "bridge", bridgeName)
		}

		// Regex Validation
		if bridge.RegexParsers.Chat == "" {
			slog.Warn("'regex_parsers.chat' is empty. In-game chat will NOT be forwarded to Discord.", "bridge", bridgeName)
		}
		if bridge.RegexParsers.Join == "" {
			slog.Warn("'regex_parsers.join' is empty. Join events will be ignored.", "bridge", bridgeName)
		}
		if bridge.RegexParsers.Leave == "" {
			slog.Warn("'regex_parsers.leave' is empty. Leave events will be ignored.", "bridge", bridgeName)
		}
	}

	return nil
}
