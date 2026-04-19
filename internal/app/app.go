// Package app wires together the configuration, Discord bot, and background services.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	disgodiscord "github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/webhook"
	"github.com/vinegod/discordgamebridge/internal/audit"
	"github.com/vinegod/discordgamebridge/internal/bot"
	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/discord"
	"github.com/vinegod/discordgamebridge/internal/executor"
	"github.com/vinegod/discordgamebridge/internal/scheduler"
	"github.com/vinegod/discordgamebridge/internal/server"
)

const auditFlushInterval = 30 * time.Second

// App holds top-level application state.
type App struct {
	configPath string
	ForceDebug bool
	reloadCh   chan struct{}
}

func New(configPath string) *App {
	return &App{
		configPath: configPath,
		reloadCh:   make(chan struct{}),
	}
}

// Run blocks until a shutdown or reload signal is received.
func (a *App) Run() error {
	rootCtx, rootCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer rootCancel()

	for {
		runCtx, cancelRun := context.WithCancel(rootCtx)
		cleanup, err := a.Start(runCtx)
		if err != nil {
			cancelRun()
			return fmt.Errorf("failed to start app: %w", err)
		}

		slog.Info("application started")

		select {
		case <-rootCtx.Done():
			slog.Info("OS interrupt received, shutting down")
			cleanup()
			cancelRun()
			return nil

		case <-a.reloadCh:
			slog.Info("reload signal received, restarting components...")
			cleanup()
			cancelRun()
		}
	}
}

func (a *App) LoadConfiguration() (*config.Config, error) {
	if a.ForceDebug {
		configureLogger("Debug")
	}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err = cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if !a.ForceDebug {
		configureLogger(cfg.Bot.LogLevel)
	}

	return cfg, nil
}

// Start initializes all components and returns a cleanup function.
func (a *App) Start(ctx context.Context) (func(), error) {
	cfg, err := a.LoadConfiguration()
	if err != nil {
		return nil, err
	}

	// Build executor registry from config
	reg, err := buildRegistry(cfg)
	if err != nil {
		return nil, fmt.Errorf("build executors: %w", err)
	}

	// Validate all executor names referenced in commands and chat config
	if err := reg.ValidateNames(cfg.ReferencedExecutorNames()); err != nil {
		reg.CloseAll()
		return nil, fmt.Errorf("config references unknown executors: %w", err)
	}

	slog.Info("initializing Discord bot")

	// Create audit log before the bot (bot records into it), but set the flush
	// function after (flush needs the bot's REST client).
	auditLog := buildAuditLog(cfg)

	discordBot, err := bot.NewBot(ctx, *cfg, a.reloadCh, reg, auditLog)
	if err != nil {
		reg.CloseAll()
		return nil, fmt.Errorf("create bot: %w", err)
	}

	if err := discordBot.Client.OpenGateway(ctx); err != nil {
		reg.CloseAll()
		return nil, fmt.Errorf("open gateway: %w", err)
	}
	slog.Info("connected to Discord gateway")

	if err := discordBot.SyncCommands(); err != nil {
		reg.CloseAll()
		return nil, fmt.Errorf("sync commands: %w", err)
	}

	if err := startAuditLog(ctx, cfg, discordBot, auditLog); err != nil {
		reg.CloseAll()
		return nil, err
	}

	sender, err := startLogTailing(ctx, cfg, discordBot)
	if err != nil {
		reg.CloseAll()
		return nil, err
	}

	var sched *scheduler.Scheduler
	if len(cfg.Schedules) > 0 {
		sched, err = scheduler.New(ctx, cfg.Schedules, reg)
		if err != nil {
			reg.CloseAll()
			return nil, fmt.Errorf("build scheduler: %w", err)
		}
		slog.Info("scheduler started", "jobs", len(cfg.Schedules))
	}

	slog.Info("bot is running — press Ctrl+C to quit")

	cleanup := func() { //nolint:contextcheck // intentional: ctx is already cancelled at shutdown; audit flush needs a fresh context
		slog.Info("shutting down components...")
		if auditLog != nil {
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			auditLog.Stop(stopCtx)
		}
		if sched != nil {
			sched.Stop()
		}
		if sender != nil {
			sender.Stop()
		}
		reg.CloseAll()
		slog.Info("cleanup complete")
	}

	return cleanup, nil
}

// buildAuditLog returns a Log if the console channel is configured, nil otherwise.
// The flush function is set later (after the bot client is available).
func buildAuditLog(cfg *config.Config) *audit.Log {
	if cfg.Server.DiscordConsoleChannelID == "" {
		return nil
	}
	return audit.New(auditFlushInterval, nil)
}

func startAuditLog(ctx context.Context, cfg *config.Config, discordBot *bot.BotWrapper, auditLog *audit.Log) error {
	if auditLog == nil {
		return nil
	}
	channelID, err := cfg.Server.ParsedConsoleChannelID()
	if err != nil {
		return fmt.Errorf("parse console channel ID for audit log: %w", err)
	}
	client := discordBot.Client
	auditLog.SetFlushFunc(func(fCtx context.Context, msg string) {
		if _, err := client.Rest.CreateMessage(channelID, disgodiscord.MessageCreate{Content: msg}); err != nil {
			slog.Warn("audit log flush failed", "error", err)
		}
	})
	auditLog.Start(ctx)
	slog.Info("command audit log enabled", "interval", auditFlushInterval)
	return nil
}

func startLogTailing(ctx context.Context, cfg *config.Config, discordBot *bot.BotWrapper) (*discord.Sender, error) {
	if cfg.Server.LogFilePath == "" {
		slog.Info("log_file_path not set, log tailing disabled")
		return nil, nil
	}
	sender, err := buildSender(ctx, cfg, discordBot)
	if err != nil {
		return nil, fmt.Errorf("build sender: %w", err)
	}
	if err := server.StartTailer(ctx, &cfg.Server, sender); err != nil {
		return nil, fmt.Errorf("start tailer: %w", err)
	}
	slog.Info("log tailer started", "file", cfg.Server.LogFilePath)
	return sender, nil
}

// buildRegistry creates an executor for each entry in cfg.Executors.
func buildRegistry(cfg *config.Config) (*executor.Registry, error) {
	reg := executor.NewRegistry()

	for name, ex := range cfg.Executors {
		switch ex.Type {
		case config.ExecutorTypeTmux:
			reg.Register(name, &executor.TmuxExecutor{
				Session: ex.Session,
				Window:  ex.Window,
				Pane:    ex.Pane,
			})
			slog.Info("registered tmux executor", "name", name, "session", ex.Session)

		case config.ExecutorTypeRcon:
			reg.Register(name, executor.NewRconExecutor(ex.Host, ex.Port, ex.Password))
			slog.Info("registered rcon executor", "name", name, "address", fmt.Sprintf("%s:%d", ex.Host, ex.Port))

		case config.ExecutorTypeScript:
			reg.Register(name, &executor.ScriptExecutor{
				AllowedDir: ex.AllowedScriptDir,
			})
			slog.Info("registered script executor", "name", name, "allowed_dir", ex.AllowedScriptDir)

		default:
			return nil, fmt.Errorf("executor %q: unsupported type %q", name, ex.Type)
		}
	}

	return reg, nil
}

// buildSender constructs and starts a single Discord message sender with all
// configured channels registered. The chat channel is the default target;
// the console/log channel is optional and registered under the "log" key.
func buildSender(ctx context.Context, cfg *config.Config, discordBot *bot.BotWrapper) (*discord.Sender, error) {
	if cfg.Server.DiscordChatChannelID == "" {
		slog.Warn("discord_chat_channel_id not set, game→Discord forwarding disabled")
		return nil, nil
	}

	channels := make(map[string]discord.ChannelTarget)

	// Chat channel (default)
	chatChannelID, err := cfg.Server.ParsedChatChannelID()
	if err != nil {
		return nil, fmt.Errorf("parse chat channel ID: %w", err)
	}
	chatTarget := discord.ChannelTarget{ChannelID: chatChannelID}
	if cfg.Server.DiscordWebhookURL != "" {
		wc, err := webhook.NewWithURL(cfg.Server.DiscordWebhookURL)
		if err != nil {
			return nil, fmt.Errorf("parse webhook URL: %w", err)
		}
		chatTarget.WebhookClient = wc
		slog.Info("chat channel webhook client configured")
	} else {
		slog.Warn("no webhook URL configured, falling back to bot messages (no player avatars)")
	}
	channels[string(config.LogChannelChat)] = chatTarget

	// Console/log channel (optional)
	if cfg.Server.DiscordConsoleChannelID != "" {
		logChannelID, err := cfg.Server.ParsedConsoleChannelID()
		if err != nil {
			return nil, fmt.Errorf("parse console channel ID: %w", err)
		}
		logTarget := discord.ChannelTarget{ChannelID: logChannelID}
		if cfg.Server.DiscordConsoleWebhookURL != "" {
			wc, err := webhook.NewWithURL(cfg.Server.DiscordConsoleWebhookURL)
			if err != nil {
				return nil, fmt.Errorf("parse log webhook URL: %w", err)
			}
			logTarget.WebhookClient = wc
			slog.Info("log channel webhook client configured")
		}
		channels[string(config.LogChannelLog)] = logTarget
	}

	sender := discord.NewSender(&discord.SenderConfig{
		Channels:      channels,
		DefaultTarget: string(config.LogChannelChat),
		BotClient:     discordBot.Client,
		FlushInterval: 500 * time.Millisecond,
		MaxBatchLines: 15,
		Workers:       2,
		RateLimit:     5,
		RateWindow:    5 * time.Second,
		MaxRetries:    3,
	})
	sender.Start(ctx)

	return sender, nil
}

func configureLogger(levelStr string) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))
}
