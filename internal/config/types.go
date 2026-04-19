// Package config defines the application's configuration structures, loading, and validation logic.
package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	snowflake "github.com/disgoorg/snowflake/v2"
)

// Config represents the root of config.yaml.
type Config struct {
	Bot       BotConfig                 `yaml:"bot"`
	Executors map[string]ExecutorConfig `yaml:"executors"`
	Server    ServerConfig              `yaml:"server"`
	Commands  []CommandConfig           `yaml:"commands"`
	Schedules []ScheduleConfig          `yaml:"schedules"`
}

// BotConfig holds Discord bot credentials.
type BotConfig struct {
	TokenEnvVar string `yaml:"token_env_var"`
	LogLevel    string `yaml:"log_level"`
	Token       string `yaml:"-"`
}

// ExecutorType identifies which transport an ExecutorConfig describes.
type ExecutorType string

const (
	ExecutorTypeTmux   ExecutorType = "tmux"
	ExecutorTypeRcon   ExecutorType = "rcon"
	ExecutorTypeScript ExecutorType = "script"
)

const (
	defaultChatTimeout     = 5 * time.Second
	defaultCommandTimeout  = 10 * time.Second
	defaultScheduleTimeout = 30 * time.Second
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

	// script fields
	AllowedScriptDir string `yaml:"allowed_script_dir"`
}

// LogChannel identifies which Discord channel a log rule routes to.
// YAML values must be the string literals "chat" or "log".
type LogChannel string

const (
	LogChannelChat LogChannel = "chat"
	LogChannelLog  LogChannel = "log"
)

// LogRuleConfig describes a single log-matching rule. Rules are evaluated in
// order; the first match wins. Ignore rules drop the line silently. All other
// rules forward a message to the configured Discord channel.
//
// Template variables available in Username and Message:
//   - {{.line}}       - the full (trimmed) log line
//   - {{.groupname}}  - any named capture group from Regex
type LogRuleConfig struct {
	Name     string `yaml:"name"`
	Regex    string `yaml:"regex"`
	Ignore   bool   `yaml:"ignore"`
	Username string `yaml:"username"`
	Message  string `yaml:"message"`
	// Channel routes the message to the chat channel (default) or the optional
	// log/audit channel configured via discord_console_channel_id.
	Channel  LogChannel     `yaml:"channel"`
	Compiled *regexp.Regexp `yaml:"-"`
}

// ServerConfig defines routing parameters, log rules, and Discord targets.
type ServerConfig struct {
	// ChatExecutor is the name of the executor used for Discord→Game chat.
	// Required when chat_template is set.
	ChatExecutor string `yaml:"chat_executor"`

	DiscordWebhookURL string

	DiscordChatChannelID     string `yaml:"discord_chat_channel_id"`
	DiscordWebhookEnv        string `yaml:"discord_webhook_env"`
	DiscordConsoleChannelID  string `yaml:"discord_console_channel_id"`
	DiscordConsoleWebhookEnv string `yaml:"discord_console_webhook_env"`
	DiscordConsoleWebhookURL string `yaml:"discord_console_webhook_url"`

	ChatTemplate string          `yaml:"chat_template"`
	ChatTimeout  time.Duration   `yaml:"chat_timeout"`
	LogFilePath  string          `yaml:"log_file_path"`
	LogRules     []LogRuleConfig `yaml:"log_rules"`
}

// CommandType identifies the execution method for a slash command.
type CommandType string

const (
	CommandTypeExecutor CommandType = "executor"
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

	// ExecutorName is the name of the executor to use.
	// Required for executor and script types.
	ExecutorName string `yaml:"executor"`

	// Template is the command string sent to the executor.
	// Supports {{.argname}} placeholders.
	// Required for executor type. Unused for script type.
	Template string `yaml:"template"`

	// ScriptPath is the script path relative to the executor's AllowedScriptDir.
	// Required for script type.
	ScriptPath string `yaml:"script_path"`

	// StaticArgs are prepended to dynamic slash command args for script commands.
	// Different commands using the same script executor can pass different static args.
	StaticArgs []string `yaml:"static_args"`

	// CommandTimeout is the per-execution deadline.
	CommandTimeout time.Duration `yaml:"script_timeout"`

	// Cooldown is the minimum time a user must wait between invocations.
	// Zero means no cooldown. Accepts Go duration strings: "5s", "1m", "30s".
	Cooldown time.Duration `yaml:"cooldown"`

	// EphemeralOutput makes the command response visible only to the invoker.
	// Defaults to true when omitted.
	EphemeralOutput *bool `yaml:"ephemeral_output"`

	// Output defines optional post-processing for executor output.
	// If nil, the raw output is used unchanged.
	Output *OutputConfig `yaml:"output"`
}

// ScheduleConfig defines a recurring task fired on a cron expression.
type ScheduleConfig struct {
	Name       string        `yaml:"name"`
	Cron       string        `yaml:"cron"`
	Executor   string        `yaml:"executor"`
	Command    string        `yaml:"command"`
	Timeout    time.Duration `yaml:"timeout"`
	SkipIfDown bool          `yaml:"skip_if_down"`
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

// OutputConfig defines optional post-processing for executor output.
// Pattern extracts named capture groups from the raw response.
// Format renders them into a human-readable string using {{.groupname}} placeholders.
type OutputConfig struct {
	Pattern string `yaml:"pattern"` // regex with named capture groups
	Format  string `yaml:"format"`  // template using {{.groupname}}

	compiled *regexp.Regexp // resolved at load time
}

// Apply transforms raw executor output using the configured pattern and format.
// Returns raw unchanged if the config is nil, the pattern didn't compile, or
// the pattern doesn't match - always better to show raw than an empty string.
func (o *OutputConfig) Apply(raw string) string {
	if o == nil || o.compiled == nil {
		return raw
	}
	groups := ExtractGroups(o.compiled, raw)
	if groups == nil {
		return raw
	}
	return SubstituteTemplate(o.Format, groups)
}

// ── Methods ───────────────────────────────────────────────────────────────────

// ReferencedExecutorNames returns every executor name referenced by commands
// and the server chat config, deduplicated. Used by the registry to validate
// all names exist before the bot starts.
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
	for idx := range c.Commands {
		add(c.Commands[idx].ExecutorName)
	}
	for idx := range c.Schedules {
		add(c.Schedules[idx].Executor)
	}
	return names
}

// ParsedChatChannelID parses DiscordChatChannelID as a Discord snowflake.
func (s *ServerConfig) ParsedChatChannelID() (snowflake.ID, error) {
	id, err := snowflake.Parse(s.DiscordChatChannelID)
	if err != nil {
		return 0, fmt.Errorf("invalid discord_chat_channel_id %q: %w", s.DiscordChatChannelID, err)
	}
	return id, nil
}

// ParsedConsoleChannelID parses DiscordConsoleChannelID as a Discord snowflake.
func (s *ServerConfig) ParsedConsoleChannelID() (snowflake.ID, error) {
	id, err := snowflake.Parse(s.DiscordConsoleChannelID)
	if err != nil {
		return 0, fmt.Errorf("invalid discord_console_channel_id %q: %w", s.DiscordConsoleChannelID, err)
	}
	return id, nil
}

// resolveType infers the CommandType from struct fields when the YAML type
// field is absent or ambiguous. Internal commands must be declared explicitly.
func resolveType(cmd *CommandConfig) CommandType {
	switch {
	case cmd.Type == CommandTypeInternal:
		return CommandTypeInternal
	case cmd.ScriptPath != "":
		return CommandTypeScript
	default:
		return CommandTypeExecutor
	}
}

// ── Shared helpers ────────────────────────────────────────────────────────────

// ExtractGroups maps a regex's named capture groups to a string map.
// Returns nil if the pattern does not match.
func ExtractGroups(re *regexp.Regexp, text string) map[string]string {
	match := re.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	results := make(map[string]string, len(re.SubexpNames()))
	for i, name := range re.SubexpNames() {
		if i != 0 && name != "" {
			results[name] = match[i]
		}
	}
	return results
}

// SubstituteTemplate replaces {{.key}} placeholders in tmpl with values from
// the provided map. Unfilled placeholders are removed. Result is trimmed.
func SubstituteTemplate(tmpl string, values map[string]string) string {
	result := tmpl
	for k, v := range values {
		result = strings.ReplaceAll(result, "{{."+k+"}}", v)
	}
	for {
		start := strings.Index(result, "{{.")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}}")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+2:]
	}
	return strings.TrimSpace(result)
}

// ── Error accumulator ─────────────────────────────────────────────────────────

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
