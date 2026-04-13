package config

import (
	"fmt"
	"os"
	"regexp"

	"github.com/joho/godotenv"
	yaml "gopkg.in/yaml.v3"
)

// Load reads the config file at configPath, resolves environment variables,
// compiles regex patterns, and returns a ready-to-use Config.
// All errors are collected via errAccumulator so the caller sees every
// problem at once rather than fixing them one at a time.
func Load(configPath string) (*Config, error) {
	_ = godotenv.Load()

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

	// Console/log channel webhook URL from env
	if cfg.Server.DiscordConsoleWebhookEnv != "" {
		cfg.Server.DiscordConsoleWebhookURL = os.Getenv(cfg.Server.DiscordConsoleWebhookEnv)
	}

	// Resolve RCON passwords from env
	for name, ex := range cfg.Executors {
		if ex.Type == ExecutorTypeRcon && ex.PasswordEnv != "" {
			ex.Password = os.Getenv(ex.PasswordEnv)
			cfg.Executors[name] = ex
		}
	}

	// Resolve command types and compile output patterns
	for idx := range cfg.Commands {
		cfg.Commands[idx].Type = resolveType(&cfg.Commands[idx])

		if cfg.Commands[idx].Output != nil {
			acc.add(compileRegex(
				cfg.Commands[idx].Name,
				cfg.Commands[idx].Output.Pattern,
				&cfg.Commands[idx].Output.compiled,
			))
		}
	}

	// Compile log rules in order
	for i := range cfg.Server.LogRules {
		rule := &cfg.Server.LogRules[i]
		if rule.Regex == "" {
			continue
		}
		re, err := regexp.Compile(rule.Regex)
		if err != nil {
			acc.add(fmt.Errorf("log_rules[%d] %q: invalid regex: %w", i, rule.Name, err))
			continue
		}
		rule.Compiled = re
	}

	if err := acc.err(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// applyDefaults fills zero values with sensible defaults before validation.
func (c *Config) applyDefaults() {
	if c.Bot.LogLevel == "" {
		c.Bot.LogLevel = "info"
	}
	if c.Server.ChatTimeout == 0 {
		c.Server.ChatTimeout = defaultChatTimeout
	}
	for i := range c.Commands {
		if c.Commands[i].CommandTimeout == 0 {
			c.Commands[i].CommandTimeout = defaultCommandTimeout
		}
	}
}

// compileRegex compiles pattern into dest. A blank pattern is a no-op.
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
