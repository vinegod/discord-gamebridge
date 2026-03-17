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

// Message is a single unit of content to be sent to Discord.
// Username and AvatarURL are used only when sending via webhook;
// they are silently ignored when falling back to the bot client.
type Message struct {
	Content   string
	Username  string // optional: display name override (webhook only)
	AvatarURL string // optional: avatar override (webhook only)
}

// SenderConfig holds all tunables for a Sender instance.
// Zero values are replaced with sensible defaults by NewSender.
type SenderConfig struct {
	// Target Discord channel. Required for the bot-client fallback path.
	ChannelID snowflake.ID

	// Optional: when set, messages are delivered as rich webhook messages
	// (custom username/avatar per message). If nil, BotClient is used.
	WebhookClient *webhook.Client

	// Required: used as fallback when WebhookClient is nil.
	BotClient *bot.Client

	// FlushInterval is how long the batcher waits to accumulate lines
	// before dispatching them to a worker as a single batch.
	// Default: 500ms.
	FlushInterval time.Duration

	// MaxBatchLines forces an early flush when the batch reaches this size,
	// regardless of FlushInterval. Prevents single large bursts from being
	// held too long.
	// Default: 15.
	MaxBatchLines int

	// Workers is the number of goroutines pulling from the work queue and
	// sending to Discord. Each worker acquires a rate-limiter token before
	// every API call, so additional workers increase throughput only when
	// the limiter allows it — they don't bypass the rate limit.
	// Default: 2.
	Workers int

	// RateLimit is the number of messages allowed per RateWindow.
	// Discord's documented limit is 5 messages per 5 seconds per channel.
	// Default: 5.
	RateLimit int

	// RateWindow is the time window for RateLimit.
	// Default: 5 seconds.
	RateWindow time.Duration

	// MaxRetries is how many times a worker retries a failed send before
	// dropping the chunk and logging an error.
	// Default: 3.
	MaxRetries int

	// MaxMessageLength is the rune cap per Discord message.
	// Default: 1900 (leaves headroom for markdown added during formatting).
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

// Sender is a goroutine-safe, rate-limited, batching Discord message sender.
//
// Architecture:
//
//	Send()
//	  │
//	  ▼
//	inbox chan Message          (buffered 256, non-blocking)
//	  │
//	  │  [batcher goroutine]
//	  │  Collects messages for FlushInterval or until MaxBatchLines,
//	  │  then dispatches a []Message batch to the work queue.
//	  ▼
//	work chan []Message         (buffered Workers*4)
//	  │
//	  ├── [worker 0] ─┐
//	  ├── [worker 1] ─┤── limiter.Wait() → doSend() → retry on 429
//	  └── [worker N] ─┘
//
// Shutdown (Stop):
//  1. close(stopCh)       signals batcher to stop accepting new messages
//  2. batcher drains inbox, dispatches remaining batches, then close(work)
//  3. workers exit via "for range work" once work is closed and empty
//  4. wg.Wait()           returns only after batcher + all workers exit
type Sender struct {
	cfg     *SenderConfig
	limiter *rate.Limiter

	inbox  chan Message   // Send() enqueues here; batcher reads
	work   chan []Message // batcher dispatches here; workers read
	stopCh chan struct{}  // closed by Stop() to signal the batcher
	wg     sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc
}

// NewSender creates a configured Sender. Call Start() before calling Send().
func NewSender(cfg *SenderConfig) *Sender {
	cfg.applyDefaults()

	// Convert the human-friendly (limit, window) pair into the tokens/second
	// rate that rate.Limiter expects.
	// Example: 5 messages / 5 seconds → Every(1s), burst 5.
	tokenRate := rate.Every(cfg.RateWindow / time.Duration(cfg.RateLimit))

	return &Sender{
		cfg:     cfg,
		limiter: rate.NewLimiter(tokenRate, cfg.RateLimit),
		inbox:   make(chan Message, 256),
		work:    make(chan []Message, cfg.Workers*4),
		stopCh:  make(chan struct{}),
	}
}

// Start launches the batcher and all worker goroutines.
// Must be called exactly once before Send().
func (s *Sender) Start(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1 + s.cfg.Workers)
	go s.batcher()
	for i := range s.cfg.Workers {
		go s.worker(ctx, i)
	}
}

// Stop signals the batcher to stop, then blocks until all workers have
// finished processing in-flight batches. Messages already in the inbox
// when Stop() is called are still delivered before returning.
func (s *Sender) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

// Send enqueues a message for batched delivery. It is non-blocking:
// if the inbox is full the message is dropped and a warning is logged.
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

// ── Internal goroutines ───────────────────────────────────────────────────────

// batcher collects incoming Messages and dispatches batches to the work queue.
// It is the sole writer to s.work and closes it on exit, which triggers the
// workers' range loops to exit once all pending batches are processed.
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
// It exits cleanly when the work channel is closed and fully drained.
// The id is used only to annotate log lines so individual workers are
// distinguishable when debugging throughput or retry storms.
func (s *Sender) worker(ctx context.Context, id int) {
	defer s.wg.Done()

	log := slog.With("worker", id, "channel", s.cfg.ChannelID)

	for batch := range s.work {
		for _, group := range groupByUsername(batch) {
			for _, chunk := range formatGroup(group, s.cfg.MaxMessageLength) {
				// Block until the shared rate limiter grants a token.
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

// ── Send pipeline ─────────────────────────────────────────────────────────────

// sendWithRetry attempts to deliver one formatted chunk, retrying on
// transient errors and honouring Discord's Retry-After on 429 responses.
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
			// Re-acquire a token after the forced sleep so the limiter's
			// internal state stays consistent with actual API calls made.
			_ = s.limiter.Wait(ctx)
			continue
		}

		// Exponential backoff for other transient errors (network, 5xx).
		time.Sleep(time.Duration(1<<attempt) * 250 * time.Millisecond)
	}

	return fmt.Errorf("send failed after %d attempts: %w", s.cfg.MaxRetries, lastErr)
}

// doSend performs exactly one API call — webhook if available, bot otherwise.
// Returns (retryAfter > 0, error) when Discord responds with 429.
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

// ── Helpers ───────────────────────────────────────────────────────────────────

// groupByUsername splits a flat message slice into runs of consecutive
// messages sharing the same Username, so a player chatting quickly appears
// as one grouped block in Discord rather than many individual messages.
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

// splitRunes breaks s into chunks of at most maxLen runes.
// Splitting by rune (not byte) prevents corrupting multi-byte UTF-8 sequences.
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
//
// TODO: replace the string check with a proper type assertion once the
// exact disgo error type is confirmed:
//
//	var restErr *rest.Error
//	if errors.As(err, &restErr) && restErr.Response.StatusCode == 429 {
//	    return time.Duration(restErr.RetryAfter * float64(time.Second))
//	}
func parseRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	if strings.Contains(err.Error(), "429") {
		return 2 * time.Second // conservative fallback until TODO above is resolved
	}
	return 0
}
