package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/disgoorg/disgo/webhook"
	"github.com/disgoorg/snowflake/v2"
	"github.com/vinegod/discordgamebridge/internal/bot"
	"github.com/vinegod/discordgamebridge/internal/bridge"
	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/discord"
)

func main() {
	slog.Info("Starting Terraria Integration Engine...")

	// 1. Load + validate configuration.
	cfg, err := config.Load("config.yaml")
	if err != nil {
		slog.Error("Fatal config error", "error", err)
		os.Exit(1)
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Bot.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))

	if err := cfg.Validate(); err != nil {
		slog.Error("Config validation failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 2. Initialize and connect the Discord bot.
	discordBot, err := bot.NewBot(ctx, *cfg)
	if err != nil {
		slog.Error("Failed to initialize Discord bot", "error", err)
		os.Exit(1)
	}
	if err = discordBot.Client.OpenGateway(context.Background()); err != nil {
		slog.Error("Failed to connect to Discord gateway", "error", err)
		os.Exit(1)
	}
	slog.Info("Connected to Discord Gateway")

	if err = discordBot.SyncCommands(); err != nil {
		slog.Error("Failed to sync slash commands", "error", err)
		os.Exit(1)
	}
	slog.Info("Slash commands synchronized")

	var senders []*discord.Sender

	for name, br := range cfg.Bridges {
		if !br.Enabled {
			continue
		}

		// Build the optional webhook client.
		var webhookClient *webhook.Client
		if br.DiscordWebhookURL != "" {
			wc, err := webhook.NewWithURL(br.DiscordWebhookURL)
			if err != nil {
				slog.Error("Failed to parse webhook URL", "bridge", name, "error", err)
				os.Exit(1)
			}
			webhookClient = wc
		}

		channelID, err := snowflake.Parse(br.DiscordChatChannelID)
		if err != nil {
			slog.Error("Invalid discord chat channel ID.", "Error", err)
			continue
		}
		// One Sender per bridge: owns rate limiting, batching, and transport
		// selection (webhook → bot fallback). The tailer just calls Send().
		sender := discord.NewSender(discord.SenderConfig{
			ChannelID:     channelID,
			WebhookClient: webhookClient,
			BotClient:     discordBot.Client,
			FlushInterval: 500 * time.Millisecond,
			MaxBatchLines: 15,
			RateLimit:     5,
			RateWindow:    5 * time.Second,
			MaxRetries:    3,
		})

		sender.Start(ctx)
		if err != nil {
			slog.Error("Failed to create sender.", "Error", err)
		} else {
			senders = append(senders, sender)
		}

		if err := bridge.StartTailer(ctx, *br, sender); err != nil {
			slog.Error("Failed to start tailer", "bridge", name, "error", err)
		} else {
			slog.Info("Started log tailer",
				"bridge", name,
				"webhook", webhookClient != nil,
			)
		}
	}

	// 4. Block until SIGINT / SIGTERM.
	<-ctx.Done()
	slog.Info("Shutting down... draining message queues.")
	for _, sender := range senders {
		sender.Stop()
	}

	discordBot.Client.Close(context.Background())
	slog.Info("Shutdown complete")
}
