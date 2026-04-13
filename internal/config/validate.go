package config

import (
	"fmt"
	"log/slog"
	"strings"
)

// Validate checks the loaded configuration for semantic errors and logs
// feature status. Returns an error if any required fields are missing or
// invalid - the application should not start with a failed Validate.
func (c *Config) Validate() error { //nolint:gocognit,gocyclo // validation is inherently branchy; extracting helpers would obscure the intent
	slog.Info("validating configuration")

	// Feature status logging
	if c.Server.LogFilePath != "" {
		slog.Info("log tailing enabled", "file", c.Server.LogFilePath)
	} else {
		slog.Info("log tailing disabled - log_file_path not set")
	}

	if c.Server.ChatTemplate != "" {
		if c.Server.ChatExecutor == "" {
			return fmt.Errorf("chat_template is set but chat_executor is missing")
		}
		slog.Info("Discord->Game chat enabled", "executor", c.Server.ChatExecutor)
	} else {
		slog.Warn("Discord->Game chat disabled - chat_template not set")
	}

	if c.Server.DiscordChatChannelID != "" {
		slog.Info("Game->Discord forwarding enabled", "channel", c.Server.DiscordChatChannelID)
	} else {
		slog.Warn("Game->Discord forwarding disabled - discord_chat_channel_id not set")
	}

	if c.Server.DiscordWebhookURL == "" && c.Server.DiscordChatChannelID != "" {
		slog.Warn("no webhook URL - messages will appear from bot account, not player names")
	}

	if c.Server.DiscordConsoleChannelID != "" {
		slog.Info("log channel enabled", "channel", c.Server.DiscordConsoleChannelID)
	}

	acc := &errAccumulator{}

	// Validate log rules
	for i, rule := range c.Server.LogRules {
		label := rule.Name
		if label == "" {
			label = fmt.Sprintf("index %d", i)
		}
		if rule.Regex == "" {
			acc.add(fmt.Errorf("log_rules[%d] %q: regex is required", i, label))
		}
		if !rule.Ignore {
			if rule.Message == "" {
				acc.add(fmt.Errorf("log_rules[%d] %q: message is required", i, label))
			}
			if rule.Channel != "" && rule.Channel != LogChannelChat && rule.Channel != LogChannelLog {
				acc.add(
					fmt.Errorf("log_rules[%d] %q: channel must be %q or %q", i, label, LogChannelChat, LogChannelLog),
				)
			}
		}
	}

	// Validate executors
	for name, ex := range c.Executors {
		acc.add(validateExecutor(name, &ex))
	}

	// Validate commands
	if len(c.Commands) > 0 {
		slog.Info("validating commands", "count", len(c.Commands))
		names := make(map[string]struct{}, len(c.Commands))
		for idx := range c.Commands {
			if _, duplicate := names[c.Commands[idx].Name]; duplicate {
				acc.add(fmt.Errorf("duplicate command name %q", c.Commands[idx].Name))
			}
			names[c.Commands[idx].Name] = struct{}{}
			acc.add(validateCommand(&c.Commands[idx]))
		}
	} else {
		slog.Info("commands disabled - none configured")
	}

	return acc.err()
}

//gocyclo:ignore
func validateCommand(cmd *CommandConfig) error {
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
	case CommandTypeExecutor:
		if cmd.ExecutorName == "" {
			acc.add(fmt.Errorf("command %q: executor is required for %s type", cmd.Name, cmd.Type))
		}
		if cmd.Template == "" {
			acc.add(fmt.Errorf("command %q: template is required for %s type", cmd.Name, cmd.Type))
		}
	case CommandTypeScript:
		if cmd.ExecutorName == "" {
			acc.add(fmt.Errorf("command %q: executor is required for script type", cmd.Name))
		}
		if cmd.ScriptPath == "" {
			acc.add(fmt.Errorf("command %q: script_path is required for script type", cmd.Name))
		}
	case CommandTypeInternal:
		// no extra fields required
	case "":
		acc.add(fmt.Errorf("command %q: type is required (executor, script, internal)", cmd.Name))
	default:
		acc.add(fmt.Errorf("command %q: unknown type %q (expected executor, script, internal)", cmd.Name, cmd.Type))
	}

	for i, arg := range cmd.Arguments {
		acc.add(validateArgument(cmd.Name, i, arg))
	}

	if cmd.Output != nil {
		if cmd.Output.Pattern == "" {
			acc.add(fmt.Errorf("command %q: output.pattern is required when output is configured", cmd.Name))
		}
		if cmd.Output.Format == "" {
			acc.add(fmt.Errorf("command %q: output.format is required when output is configured", cmd.Name))
		}
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
		acc.add(fmt.Errorf(
			"command %q argument %q: description exceeds 100 character Discord limit",
			cmdName, arg.Name,
		))
	}

	switch arg.Type {
	case VariableTypeString, VariableTypeBool:
		// valid
	case "":
		acc.add(fmt.Errorf("command %q argument %q: type is required (string, boolean)", cmdName, arg.Name))
	default:
		acc.add(fmt.Errorf(
			"command %q argument %q: unknown type %q (expected string, boolean)",
			cmdName, arg.Name, arg.Type,
		))
	}

	return acc.err()
}

func validateExecutor(name string, cfg *ExecutorConfig) error {
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
	case ExecutorTypeScript:
		if cfg.AllowedScriptDir == "" {
			acc.add(fmt.Errorf("executor %q: allowed_script_dir is required for script type", name))
		}
	case "":
		acc.add(fmt.Errorf("executor %q: type is required (tmux, rcon, script)", name))
	default:
		acc.add(fmt.Errorf("executor %q: unknown type %q (expected tmux, rcon, script)", name, cfg.Type))
	}

	return acc.err()
}
