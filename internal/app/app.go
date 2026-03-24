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

	"github.com/disgoorg/disgo/webhook"
	"github.com/vinegod/discordgamebridge/internal/bot"
	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/discord"
	"github.com/vinegod/discordgamebridge/internal/server"
)

// internal/app/app.go
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

// Runs app and blocks until a reload/shutdown signal is received.
func (a *App) Run() error {
	rootCtx, rootCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer rootCancel()

	for {
		runCtx, cancelRun := context.WithCancel(rootCtx)
		cleanup, err := a.Start(runCtx)
		if err != nil {
			cancelRun()
			return fmt.Errorf("Failed to start app: %w", err)
		}

		slog.Info("Application started")

		// Block until the OS stops the app, OR a reload is requested
		select {
		case <-rootCtx.Done():
			slog.Info("OS interrupt received, shutting down")
			cleanup()
			cancelRun()
			return nil

		case <-a.reloadCh:
			slog.Info("Reload signal received, restarting components...")
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

// Start initializes the application and returns cleanup function
func (a *App) Start(ctx context.Context) (func(), error) {
	cfg, err := a.LoadConfiguration()
	if err != nil {
		return nil, err
	}

	slog.Info("initializing Discord bot")

	discordBot, err := bot.NewBot(ctx, *cfg, a.reloadCh)
	if err != nil {
		return nil, fmt.Errorf("create bot: %w", err)
	}

	if err := discordBot.Client.OpenGateway(ctx); err != nil {
		return nil, fmt.Errorf("open gateway: %w", err)
	}
	slog.Info("connected to Discord gateway")

	if err := discordBot.SyncCommands(); err != nil {
		return nil, fmt.Errorf("sync commands: %w", err)
	}
	slog.Info("slash commands synchronized")

	var sender *discord.Sender
	if cfg.Server.LogFilePath != "" {
		sender, err = buildSender(ctx, cfg, discordBot)
		if err != nil {
			return nil, fmt.Errorf("build sender: %w", err)
		}
		if err := server.StartTailer(ctx, &cfg.Server, sender); err != nil {
			return nil, fmt.Errorf("start tailer: %w", err)
		}
		slog.Info("log tailer started", "file", cfg.Server.LogFilePath)
	} else {
		slog.Info("log_file_path not set, log tailing disabled")
	}

	slog.Info("bot is running — press Ctrl+C to quit")

	cleanup := func() {
		slog.Info("shutting down components...")
		if sender != nil {
			sender.Stop()
		}
		slog.Info("cleanup complete")
	}

	return cleanup, nil
}

// buildSender constructs and starts the Discord message sender for the configured server channel.
func buildSender(ctx context.Context, cfg *config.Config, discordBot *bot.BotWrapper) (*discord.Sender, error) {
	if cfg.Server.DiscordChatChannelID == "" {
		slog.Warn("discord_chat_channel_id not set, game→Discord forwarding disabled")
		return nil, nil
	}

	var webhookClient *webhook.Client
	if cfg.Server.DiscordWebhookURL != "" {
		wc, err := webhook.NewWithURL(cfg.Server.DiscordWebhookURL)
		if err != nil {
			return nil, fmt.Errorf("parse webhook URL: %w", err)
		}
		webhookClient = wc
		slog.Info("webhook client configured")
	} else {
		slog.Warn("no webhook URL configured, falling back to bot messages (no player avatars)")
	}

	channelID, err := cfg.Server.ParsedChatChannelID()
	if err != nil {
		return nil, fmt.Errorf("parse chat channel ID: %w", err)
	}

	sender := discord.NewSender(&discord.SenderConfig{
		ChannelID:     channelID,
		WebhookClient: webhookClient,
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

// configureLogger sets the global slog handler, defaulting to Info if the provided level is invalid.
func configureLogger(levelStr string) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))
}
