package config

import (
	"fmt"
	"log/slog"
	"strings"
)

// Validate checks the loaded configuration for semantic errors and logs
// feature status. Returns an error if any required fields are missing or
// invalid - the application should not start with a failed Validate.
func (c *Config) Validate() error {
	slog.Info("validating configuration")

	if err := logFeatureStatus(&c.Server); err != nil {
		return err
	}

	acc := &errAccumulator{}
	acc.add(validateLogRules(c.Server.LogRules))
	acc.add(validateSchedules(c.Schedules))
	for name, ex := range c.Executors {
		acc.add(validateExecutor(name, &ex))
	}
	acc.add(validateCommands(c.Commands))

	return acc.err()
}

func logFeatureStatus(s *ServerConfig) error {
	if s.LogFilePath != "" {
		slog.Info("log tailing enabled", "file", s.LogFilePath)
	} else {
		slog.Info("log tailing disabled - log_file_path not set")
	}

	if s.ChatTemplate != "" {
		if s.ChatExecutor == "" {
			return fmt.Errorf("chat_template is set but chat_executor is missing")
		}
		slog.Info("Discord->Game chat enabled", "executor", s.ChatExecutor)
	} else {
		slog.Warn("Discord->Game chat disabled - chat_template not set")
	}

	if s.DiscordChatChannelID != "" {
		slog.Info("Game->Discord forwarding enabled", "channel", s.DiscordChatChannelID)
	} else {
		slog.Warn("Game->Discord forwarding disabled - discord_chat_channel_id not set")
	}

	if s.DiscordWebhookURL == "" && s.DiscordChatChannelID != "" {
		slog.Warn("no webhook URL - messages will appear from bot account, not player names")
	}

	if s.DiscordConsoleChannelID != "" {
		slog.Info("log channel enabled", "channel", s.DiscordConsoleChannelID)
	}

	return nil
}

func validateLogRules(rules []LogRuleConfig) error {
	acc := &errAccumulator{}
	for i, rule := range rules {
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
	return acc.err()
}

func validateSchedules(schedules []ScheduleConfig) error {
	acc := &errAccumulator{}
	for i, sched := range schedules {
		label := sched.Name
		if label == "" {
			label = fmt.Sprintf("index %d", i)
		}
		if sched.Cron == "" {
			acc.add(fmt.Errorf("schedules[%d] %q: cron is required", i, label))
		}
		if sched.Executor == "" {
			acc.add(fmt.Errorf("schedules[%d] %q: executor is required", i, label))
		}
		if sched.Command == "" {
			acc.add(fmt.Errorf("schedules[%d] %q: command is required", i, label))
		}
		if sched.Timeout < 0 {
			acc.add(fmt.Errorf("schedules[%d] %q: timeout must not be negative", i, label))
		}
	}
	return acc.err()
}

func validateCommands(commands []CommandConfig) error {
	if len(commands) == 0 {
		slog.Info("commands disabled - none configured")
		return nil
	}

	slog.Info("validating commands", "count", len(commands))
	acc := &errAccumulator{}
	names := make(map[string]struct{}, len(commands))
	for idx := range commands {
		if _, duplicate := names[commands[idx].Name]; duplicate {
			acc.add(fmt.Errorf("duplicate command name %q", commands[idx].Name))
		}
		names[commands[idx].Name] = struct{}{}
		acc.add(validateCommand(&commands[idx]))
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

	for i := range cmd.Arguments {
		acc.add(validateArgument(cmd.Name, i, &cmd.Arguments[i]))
	}

	if cmd.Cooldown < 0 {
		acc.add(fmt.Errorf("command %q: cooldown must not be negative", cmd.Name))
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

func validateArgument(cmdName string, idx int, arg *ArgumentConfig) error {
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
	case VariableTypeString:
		acc.add(validateStringArgConstraints(cmdName, arg.Name, arg))
	case VariableTypeBool:
		if arg.MinLength != 0 || arg.MaxLength != 0 || arg.Pattern != "" {
			acc.add(fmt.Errorf(
				"command %q argument %q: min_length/max_length/pattern only apply to string arguments",
				cmdName, arg.Name,
			))
		}
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

func validateStringArgConstraints(cmdName, argName string, arg *ArgumentConfig) error {
	acc := &errAccumulator{}
	if arg.MinLength < 0 {
		acc.add(fmt.Errorf("command %q argument %q: min_length must not be negative", cmdName, argName))
	}
	if arg.MaxLength < 0 {
		acc.add(fmt.Errorf("command %q argument %q: max_length must not be negative", cmdName, argName))
	}
	if arg.MinLength > 0 && arg.MaxLength > 0 && arg.MaxLength < arg.MinLength {
		acc.add(fmt.Errorf("command %q argument %q: max_length must be >= min_length", cmdName, argName))
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
