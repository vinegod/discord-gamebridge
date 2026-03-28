// Package config defines the application's configuration structures and loading logic.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	snowflake "github.com/disgoorg/snowflake/v2"
	"github.com/joho/godotenv"
	yaml "gopkg.in/yaml.v3"
)

// Config represents the root of config.yaml.
type Config struct {
	Bot       BotConfig                 `yaml:"bot"`
	Executors map[string]ExecutorConfig `yaml:"executors"`
	Server    ServerConfig              `yaml:"server"`
	Commands  []CommandConfig           `yaml:"commands"`
}

// BotConfig holds Discord bot credentials and script execution policies.
type BotConfig struct {
	TokenEnvVar      string `yaml:"token_env_var"`
	LogLevel         string `yaml:"log_level"`
	AllowedScriptDir string `yaml:"allowed_script_dir"`
	Token            string `yaml:"-"`
}

// ExecutorType identifies which transport an ExecutorConfig describes.
type ExecutorType string

const (
	ExecutorTypeTmux ExecutorType = "tmux"
	ExecutorTypeRcon ExecutorType = "rcon"
)

// ExecutorConfig describes a named executor entry in config.yaml.
// Only the fields relevant to the chosen Type need to be set.
type ExecutorConfig struct {
	Type ExecutorType `yaml:"type"`

	// tmux fields
	Session string `yaml:"session"`
	Window  int    `yaml:"window"`
	Pane    int    `yaml:"pane"`

	// rcon fields
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	PasswordEnv string `yaml:"password_env"` // name of the env var holding the password
	Password    string `yaml:"-"`            // resolved at load time
}

// ServerConfig defines routing parameters, regex parsers, and Discord targets.
type ServerConfig struct {
	// ChatExecutor is the name of the executor used for Discord→Game chat.
	// Required when chat_template is set.
	ChatExecutor string `yaml:"chat_executor"`

	DiscordWebhookURL string

	DiscordChatChannelID     string `yaml:"discord_chat_channel_id"`
	DiscordWebhookEnv        string `yaml:"discord_webhook_env"`
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
	CompiledEvents  *regexp.Regexp `yaml:"-"`
	CompiledIgnore  *regexp.Regexp `yaml:"-"`
}

// RegexParsers holds raw regular expression strings for log matching.
type RegexParsers struct {
	Chat    string `yaml:"chat"`
	Join    string `yaml:"join"`
	Leave   string `yaml:"leave"`
	Console string `yaml:"console"`
	Events  string `yaml:"events"`
	Ignore  string `yaml:"ignore"`
}

// CommandType identifies the execution method for a slash command.
type CommandType string

const (
	CommandTypeTmux     CommandType = "tmux"
	CommandTypeRcon     CommandType = "rcon"
	CommandTypeScript   CommandType = "script"
	CommandTypeInternal CommandType = "internal"
)

// CommandConfig defines an executable slash command.
type CommandConfig struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Type        CommandType      `yaml:"type"`
	Permissions PermissionConfig `yaml:"permissions"`
	Arguments   []ArgumentConfig `yaml:"arguments"`

	ExecutorName string `yaml:"executor"`

	// Template is the command string sent to the executor.
	// Supports {{.argname}} placeholders.
	Template string `yaml:"template"`

	// Script-specific fields (type: script only).
	ScriptPath     string        `yaml:"script_path"`
	StaticArgs     []string      `yaml:"static_args"`
	CommandTimeout time.Duration `yaml:"script_timeout"`
}

// PermissionConfig defines access control lists for a command.
type PermissionConfig struct {
	AllowedRoles []string `yaml:"allowed_roles"`
	AllowedUsers []string `yaml:"allowed_users"`
}

// VariableType is the Discord option type for a command argument.
type VariableType string

const (
	VariableTypeBool   VariableType = "boolean"
	VariableTypeString VariableType = "string"
)

// ArgumentConfig defines a single slash command option.
type ArgumentConfig struct {
	Name        string       `yaml:"name"`
	Type        VariableType `yaml:"type"`
	Description string       `yaml:"description"`
	Required    bool         `yaml:"required"`
}

// ReferencedExecutorNames returns every executor name referenced by commands and the server chat config.
func (c *Config) ReferencedExecutorNames() []string {
	seen := make(map[string]struct{})
	var names []string

	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}

	add(c.Server.ChatExecutor)
	for _, cmd := range c.Commands {
		add(cmd.ExecutorName)
	}
	return names
}

func (c *Config) applyDefaults() {
	if c.Bot.LogLevel == "" {
		c.Bot.LogLevel = "info"
	}
	if c.Server.ChatTimeout == 0 {
		c.Server.ChatTimeout = 5 * time.Second
	}
	for i := range c.Commands {
		if c.Commands[i].CommandTimeout == 0 {
			c.Commands[i].CommandTimeout = 10 * time.Second
		}
	}
}

type errAccumulator struct {
	errs []error
}

func (e *errAccumulator) add(err error) {
	if err != nil {
		e.errs = append(e.errs, err)
	}
}

func (e *errAccumulator) err() error {
	return errors.Join(e.errs...)
}

func compileRegex(name, pattern string, dest **regexp.Regexp) error {
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid %s regex: %w", name, err)
	}
	*dest = re
	return nil
}

func validateCommand(cmd CommandConfig) error {
	acc := &errAccumulator{}

	switch {
	case cmd.Name == "":
		acc.add(fmt.Errorf("name is required"))
	case len(cmd.Name) > 32:
		acc.add(fmt.Errorf("command %q: name exceeds 32 character Discord limit", cmd.Name))
	case strings.ContainsAny(cmd.Name, " \t\n\r"):
		acc.add(fmt.Errorf("command %q: name must not contain whitespace", cmd.Name))
	case strings.ToLower(cmd.Name) != cmd.Name:
		acc.add(fmt.Errorf("command %q: name must be lowercase", cmd.Name))
	}

	switch {
	case cmd.Description == "":
		acc.add(fmt.Errorf("command %q: description is required", cmd.Name))
	case len(cmd.Description) > 100:
		acc.add(fmt.Errorf("command %q: description exceeds 100 character Discord limit", cmd.Name))
	}

	switch cmd.Type {
	case CommandTypeTmux, CommandTypeRcon:
		if cmd.ExecutorName == "" {
			acc.add(fmt.Errorf("command %q: executor is required for %s type", cmd.Name, cmd.Type))
		}
		if cmd.Template == "" {
			acc.add(fmt.Errorf("command %q: template is required for %s type", cmd.Name, cmd.Type))
		}
	case CommandTypeScript:
		if cmd.ScriptPath == "" {
			acc.add(fmt.Errorf("command %q: script_path is required for script type", cmd.Name))
		}
	case CommandTypeInternal:
		// no extra fields required
	case "":
		acc.add(fmt.Errorf("command %q: type is required (tmux, rcon, script, internal)", cmd.Name))
	default:
		acc.add(fmt.Errorf("command %q: unknown type %q (expected tmux, rcon, script, internal)", cmd.Name, cmd.Type))
	}

	for i, arg := range cmd.Arguments {
		acc.add(validateArgument(cmd.Name, i, arg))
	}

	return acc.err()
}

func validateArgument(cmdName string, idx int, arg ArgumentConfig) error {
	acc := &errAccumulator{}

	switch {
	case arg.Name == "":
		acc.add(fmt.Errorf("command %q argument[%d]: name is required", cmdName, idx))
	case strings.ContainsAny(arg.Name, " \t\n\r"):
		acc.add(fmt.Errorf("command %q argument %q: name must not contain whitespace", cmdName, arg.Name))
	}

	switch {
	case arg.Description == "":
		acc.add(fmt.Errorf("command %q argument %q: description is required", cmdName, arg.Name))
	case len(arg.Description) > 100:
		acc.add(fmt.Errorf("command %q argument %q: description exceeds 100 character Discord limit", cmdName, arg.Name))
	}

	switch arg.Type {
	case VariableTypeString, VariableTypeBool:
		// valid
	case "":
		acc.add(fmt.Errorf("command %q argument %q: type is required (string, boolean)", cmdName, arg.Name))
	default:
		acc.add(fmt.Errorf("command %q argument %q: unknown type %q (expected string, boolean)", cmdName, arg.Name, arg.Type))
	}

	return acc.err()
}

func validateExecutor(name string, cfg ExecutorConfig) error {
	acc := &errAccumulator{}

	switch cfg.Type {
	case ExecutorTypeTmux:
		if cfg.Session == "" {
			acc.add(fmt.Errorf("executor %q: session is required for tmux type", name))
		}
	case ExecutorTypeRcon:
		if cfg.Host == "" {
			acc.add(fmt.Errorf("executor %q: host is required for rcon type", name))
		}
		if cfg.Port == 0 {
			acc.add(fmt.Errorf("executor %q: port is required for rcon type", name))
		}
		if cfg.Password == "" {
			acc.add(fmt.Errorf("executor %q: password_env must point to a non-empty env var", name))
		}
	case "":
		acc.add(fmt.Errorf("executor %q: type is required (tmux, rcon)", name))
	default:
		acc.add(fmt.Errorf("executor %q: unknown type %q (expected tmux, rcon)", name, cfg.Type))
	}

	return acc.err()
}

func Load(configPath string) (*Config, error) {
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

	acc := &errAccumulator{}

	// Bot token
	token := os.Getenv(cfg.Bot.TokenEnvVar)
	if token == "" {
		acc.add(fmt.Errorf("discord token env var [%s] is empty", cfg.Bot.TokenEnvVar))
	}
	cfg.Bot.Token = token

	// Webhook URL from env
	if cfg.Server.DiscordWebhookEnv != "" {
		cfg.Server.DiscordWebhookURL = os.Getenv(cfg.Server.DiscordWebhookEnv)
	}

	// Resolve RCON passwords from env
	for name, ex := range cfg.Executors {
		if ex.Type == ExecutorTypeRcon && ex.PasswordEnv != "" {
			ex.Password = os.Getenv(ex.PasswordEnv)
			cfg.Executors[name] = ex
		}
	}

	// Compile regex patterns
	acc.add(compileRegex("chat", cfg.Server.RegexParsers.Chat, &cfg.Server.CompiledChat))
	acc.add(compileRegex("join", cfg.Server.RegexParsers.Join, &cfg.Server.CompiledJoin))
	acc.add(compileRegex("leave", cfg.Server.RegexParsers.Leave, &cfg.Server.CompiledLeave))
	acc.add(compileRegex("console", cfg.Server.RegexParsers.Console, &cfg.Server.CompiledConsole))
	acc.add(compileRegex("events", cfg.Server.RegexParsers.Events, &cfg.Server.CompiledEvents))
	acc.add(compileRegex("ignore", cfg.Server.RegexParsers.Ignore, &cfg.Server.CompiledIgnore))

	if err := acc.err(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	slog.Info("validating configuration")

	// Feature status
	if c.Server.LogFilePath != "" {
		slog.Info("log tailing enabled", "file", c.Server.LogFilePath)
	} else {
		slog.Info("log tailing disabled — log_file_path not set")
	}

	if c.Server.ChatTemplate != "" {
		if c.Server.ChatExecutor == "" {
			return fmt.Errorf("chat_template is set but chat_executor is missing")
		}
		slog.Info("Discord→Game chat enabled", "executor", c.Server.ChatExecutor)
	} else {
		slog.Warn("Discord→Game chat disabled — chat_template not set")
	}

	if c.Server.DiscordChatChannelID != "" {
		slog.Info("Game→Discord forwarding enabled", "channel", c.Server.DiscordChatChannelID)
	} else {
		slog.Warn("Game→Discord forwarding disabled — discord_chat_channel_id not set")
	}

	if c.Server.DiscordWebhookURL == "" && c.Server.DiscordChatChannelID != "" {
		slog.Warn("no webhook URL — messages will appear from bot account, not player names")
	}

	acc := &errAccumulator{}

	// Validate executors
	for name, ex := range c.Executors {
		acc.add(validateExecutor(name, ex))
	}

	// Validate commands
	if len(c.Commands) > 0 {
		slog.Info("validating commands", "count", len(c.Commands))
		names := make(map[string]struct{}, len(c.Commands))
		for _, cmd := range c.Commands {
			if _, duplicate := names[cmd.Name]; duplicate {
				acc.add(fmt.Errorf("duplicate command name %q", cmd.Name))
			}
			names[cmd.Name] = struct{}{}
			acc.add(validateCommand(cmd))
		}
	} else {
		slog.Info("commands disabled — none configured")
	}

	return acc.err()
}

func (s *ServerConfig) ParsedChatChannelID() (snowflake.ID, error) {
	id, err := snowflake.Parse(s.DiscordChatChannelID)
	if err != nil {
		return 0, fmt.Errorf("invalid discord_chat_channel_id %q: %w", s.DiscordChatChannelID, err)
	}
	return id, nil
}
