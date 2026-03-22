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
	ConfigPath string
	ForceDebug bool
	ReloadCh   chan struct{}
}

func New(configPath string) *App {
	return &App{
		ConfigPath: configPath,
		ReloadCh:   make(chan struct{}, 1),
	}
}

func (a *App) Run() error {
	rootCtx, rootCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer rootCancel()

	for {
		runCtx, cancelRun := context.WithCancel(rootCtx)

		if err := a.Start(runCtx); err != nil {
			cancelRun()
			return fmt.Errorf("Failed to start app: %v", err)
		}

		slog.Info("Application started")

		// Block until the OS stops the app, OR a reload is requested
		select {
		case <-rootCtx.Done():
			slog.Info("OS interrupt received, shutting down")
			cancelRun()
			return nil

		case <-a.ReloadCh:
			slog.Info("Reload signal received, restarting components...")
			cancelRun()
		}
	}
}

func (a *App) LoadConfiguration() (*config.Config, error) {
	if a.ForceDebug {
		configureLogger("Debug")
	}

	cfg, err := config.Load(a.ConfigPath)
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

// Run initializes the application and blocks until a shutdown signal is received.
func (a *App) Start(ctx context.Context) error {
	cfg, err := a.LoadConfiguration()
	if err != nil {
		return err
	}

	slog.Info("initializing Discord bot")

	discordBot, err := bot.NewBot(ctx, *cfg, a.ReloadCh)
	if err != nil {
		return fmt.Errorf("create bot: %w", err)
	}

	if err := discordBot.Client.OpenGateway(ctx); err != nil {
		return fmt.Errorf("open gateway: %w", err)
	}

	// Ensure the gateway is closed on exit regardless of how we got here.
	defer func() {
		slog.Info("closing Discord gateway")
		discordBot.Client.Close(ctx)
	}()

	slog.Info("connected to Discord gateway")

	if err := discordBot.SyncCommands(); err != nil {
		return fmt.Errorf("sync commands: %w", err)
	}
	slog.Info("slash commands synchronized")

	sender, err := buildSender(ctx, cfg, discordBot)
	if err != nil {
		return fmt.Errorf("build sender: %w", err)
	}
	defer func() {
		slog.Info("draining message queue")
		sender.Stop()
	}()

	if err := server.StartTailer(ctx, &cfg.Server, sender); err != nil {
		return fmt.Errorf("start tailer: %w", err)
	}
	slog.Info("log tailer started", "file", cfg.Server.LogFilePath)

	slog.Info("bot is running — press Ctrl+C to quit")
	<-ctx.Done()

	slog.Info("shutdown signal received, cleaning up...")
	return nil
}

// buildSender constructs and starts the Discord message sender for the configured server channel.
func buildSender(ctx context.Context, cfg *config.Config, discordBot *bot.BotWrapper) (*discord.Sender, error) {
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
