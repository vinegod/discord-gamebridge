package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/disgo/webhook"
	snowflake "github.com/disgoorg/snowflake/v2"
	"golang.org/x/time/rate"
)

const SystemUsername = "System"

type MessageSender interface {
	Send(msg Message)
}

// Message is a single unit of content to be sent to Discord.
// Username and AvatarURL are used only when sending via webhook;
// they are silently ignored when falling back to the bot client.
type Message struct {
	Content   string
	Username  string // optional: display name override (webhook only)
	AvatarURL string // optional: avatar override (webhook only)
}

// SenderConfig defines the tuning parameters for the batched message dispatcher.
type SenderConfig struct {
	ChannelID        snowflake.ID
	WebhookClient    *webhook.Client
	BotClient        *bot.Client
	FlushInterval    time.Duration
	MaxBatchLines    int
	Workers          int
	RateLimit        int
	RateWindow       time.Duration
	MaxRetries       int
	MaxMessageLength int
}

func (c *SenderConfig) applyDefaults() {
	if c.FlushInterval == 0 {
		c.FlushInterval = 500 * time.Millisecond
	}
	if c.MaxBatchLines == 0 {
		c.MaxBatchLines = 15
	}
	if c.Workers == 0 {
		c.Workers = 2
	}
	if c.RateLimit == 0 {
		c.RateLimit = 5
	}
	if c.RateWindow == 0 {
		c.RateWindow = 5 * time.Second
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
	if c.MaxMessageLength == 0 {
		c.MaxMessageLength = 1900
	}
}

// Sender is a goroutine-safe, rate-limited, batching Discord message dispatcher.
type Sender struct {
	cfg     *SenderConfig
	limiter *rate.Limiter

	inbox  chan Message
	work   chan []Message
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewSender(cfg *SenderConfig) *Sender {
	cfg.applyDefaults()
	tokenRate := rate.Every(cfg.RateWindow / time.Duration(cfg.RateLimit))

	return &Sender{
		cfg:     cfg,
		limiter: rate.NewLimiter(tokenRate, cfg.RateLimit),
		inbox:   make(chan Message, 256),
		work:    make(chan []Message, cfg.Workers*4),
		stopCh:  make(chan struct{}),
	}
}

// Start launches the batcher and worker goroutines.
func (s *Sender) Start(ctx context.Context) {
	workerCtx := context.WithoutCancel(ctx)
	s.wg.Add(1 + s.cfg.Workers)
	go s.batcher()
	for i := range s.cfg.Workers {
		go s.worker(workerCtx, i)
	}
}

// Stop closes the internal queues and blocks until pending messages are sent.
func (s *Sender) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

// Send enqueues a message for delivery. It drops the message if the buffer is full.
func (s *Sender) Send(msg Message) {
	select {
	case s.inbox <- msg:
	default:
		slog.Warn("sender inbox full, dropping message",
			"channel", s.cfg.ChannelID,
			"username", msg.Username,
		)
	}
}

// batcher collects incoming Messages and dispatches batches to the work queue.
func (s *Sender) batcher() {
	defer s.wg.Done()
	defer close(s.work)

	ticker := time.NewTicker(s.cfg.FlushInterval)
	defer ticker.Stop()

	var batch []Message

	dispatch := func() {
		if len(batch) == 0 {
			return
		}
		s.work <- batch
		batch = nil
	}

	for {
		select {
		case <-s.stopCh:
			// Drain the inbox completely before exiting so no messages
			// queued before Stop() was called are lost.
			for {
				select {
				case msg := <-s.inbox:
					batch = append(batch, msg)
				default:
					dispatch()
					return
				}
			}

		case msg := <-s.inbox:
			batch = append(batch, msg)
			if len(batch) >= s.cfg.MaxBatchLines {
				dispatch()
				ticker.Reset(s.cfg.FlushInterval)
			}

		case <-ticker.C:
			dispatch()
		}
	}
}

// worker pulls batches from the work queue and delivers them to Discord.
func (s *Sender) worker(ctx context.Context, id int) {
	defer s.wg.Done()

	log := slog.With("worker", id, "channel", s.cfg.ChannelID)

	for batch := range s.work {
		for _, group := range groupByUsername(batch) {
			for _, chunk := range formatGroup(group, s.cfg.MaxMessageLength) {
				if err := s.limiter.Wait(ctx); err != nil {
					// Only occurs if context is cancelled or burst is exceeded
					log.Error("rate limiter error, skipping chunk", "error", err)
					continue
				}

				if err := s.sendWithRetry(ctx, log, group[0], chunk); err != nil {
					log.Error("failed to deliver message",
						"error", err,
						"username", group[0].Username,
					)
				}
			}
		}
	}
}

func (s *Sender) sendWithRetry(ctx context.Context, log *slog.Logger, representative Message, content string) error {
	var lastErr error

	for attempt := range s.cfg.MaxRetries + 1 {
		if attempt > 0 {
			log.Warn("retrying send",
				"attempt", attempt,
				"max", s.cfg.MaxRetries,
				"error", lastErr,
			)
		}

		retryAfter, err := s.doSend(representative, content)
		if err == nil {
			return nil
		}
		lastErr = err

		if retryAfter > 0 {
			// Discord told us the exact wait time.
			log.Warn("rate limited by Discord, waiting",
				"retry_after", retryAfter,
				"attempt", attempt,
			)
			time.Sleep(retryAfter)
			_ = s.limiter.Wait(ctx)
			continue
		}

		// Exponential backoff for other transient errors (network, 5xx).
		time.Sleep(time.Duration(1<<attempt) * 250 * time.Millisecond)
	}

	return fmt.Errorf("send failed after %d attempts: %w", s.cfg.MaxRetries, lastErr)
}

// doSend routes the API call to the webhook or bot client. Returns (retryAfter > 0, error) on HTTP 429.
func (s *Sender) doSend(msg Message, content string) (time.Duration, error) {
	if s.cfg.WebhookClient != nil {
		return s.sendViaWebhook(msg, content)
	}
	return s.sendViaBot(content)
}

func (s *Sender) sendViaWebhook(msg Message, content string) (time.Duration, error) {
	payload := discord.WebhookMessageCreate{Content: content}
	if msg.Username != "" {
		payload.Username = msg.Username
	}
	if msg.AvatarURL != "" {
		payload.AvatarURL = msg.AvatarURL
	}

	_, err := (*s.cfg.WebhookClient).CreateMessage(payload, rest.CreateWebhookMessageParams{})
	if err != nil {
		return parseRetryAfter(err), fmt.Errorf("webhook send: %w", err)
	}
	return 0, nil
}

func (s *Sender) sendViaBot(content string) (time.Duration, error) {
	_, err := s.cfg.BotClient.Rest.CreateMessage(s.cfg.ChannelID, discord.MessageCreate{
		Content: content,
	})
	if err != nil {
		return parseRetryAfter(err), fmt.Errorf("bot send: %w", err)
	}
	return 0, nil
}

// groupByUsername splits a flat message slice into runs of consecutive messages
func groupByUsername(msgs []Message) [][]Message {
	if len(msgs) == 0 {
		return nil
	}
	var groups [][]Message
	current := []Message{msgs[0]}

	for _, msg := range msgs[1:] {
		if msg.Username == current[0].Username {
			current = append(current, msg)
		} else {
			groups = append(groups, current)
			current = []Message{msg}
		}
	}
	return append(groups, current)
}

// formatGroup joins a group into a single string and applies formatting:
//   - No Username → server log output → wrapped in a code block.
//   - Has Username → in-game chat → plain text (looks conversational).
//
// The result is split into maxLen-rune chunks to respect Discord's limit.
func formatGroup(group []Message, maxLen int) []string {
	lines := make([]string, len(group))
	for i, m := range group {
		lines[i] = m.Content
	}
	joined := strings.Join(lines, "\n")

	if group[0].Username == "" || group[0].Username == SystemUsername {
		joined = "```\n" + joined + "\n```"
	}

	return splitMessage(joined, maxLen)
}

// splitMessage breaks a string into chunks by rune count.
func splitMessage(s string, limit int) []string {
	var parts []string
	var b strings.Builder
	runeCount := 0

	for _, r := range s {
		if runeCount+1 > limit {
			parts = append(parts, b.String())
			b.Reset()
			runeCount = 0
		}
		b.WriteRune(r)
		runeCount++
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts
}

// parseRetryAfter extracts a Retry-After duration from a Discord error.
// Returns 0 if the error is not a 429.
func parseRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	if strings.Contains(err.Error(), "429") {
		return 2 * time.Second // conservative fallback until TODO above is resolved
	}
	return 0
}
