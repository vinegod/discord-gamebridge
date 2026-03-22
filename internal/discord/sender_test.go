package discord

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// --- splitMessage ---

func TestSplitMessage_EmptyString(t *testing.T) {
	if parts := splitMessage("", 100); len(parts) != 0 {
		t.Errorf("expected no parts for empty string, got %d", len(parts))
	}
}

func TestSplitMessage_BelowLimit(t *testing.T) {
	parts := splitMessage("hello", 100)
	if len(parts) != 1 || parts[0] != "hello" {
		t.Errorf("expected single unchanged part, got %v", parts)
	}
}

func TestSplitMessage_ExactlyAtLimit(t *testing.T) {
	s := strings.Repeat("a", 100)
	parts := splitMessage(s, 100)
	if len(parts) != 1 {
		t.Errorf("expected 1 part at exact limit, got %d", len(parts))
	}
}

func TestSplitMessage_OneOverLimit(t *testing.T) {
	s := strings.Repeat("a", 101)
	parts := splitMessage(s, 100)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if len([]rune(parts[0])) != 100 || len([]rune(parts[1])) != 1 {
		t.Errorf("unexpected split sizes: first=%d last=%d", len([]rune(parts[0])), len([]rune(parts[1])))
	}
}

func TestSplitMessage_MultipleChunks(t *testing.T) {
	s := strings.Repeat("x", 250)
	parts := splitMessage(s, 100)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts (100+100+50), got %d", len(parts))
	}
	if len([]rune(parts[2])) != 50 {
		t.Errorf("last part should be 50 runes, got %d", len([]rune(parts[2])))
	}
}

func TestSplitMessage_MultibyteRunes_NotCorrupted(t *testing.T) {
	// "日" is 3 bytes but 1 rune. Byte-based slicing would produce invalid UTF-8.
	s := strings.Repeat("日", 100)
	parts := splitMessage(s, 60)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts for 100 CJK runes at limit 60, got %d", len(parts))
	}
	for i, part := range parts {
		for _, r := range part {
			if r == '\uFFFD' {
				t.Errorf("part %d contains UTF-8 replacement character — was byte-sliced", i)
			}
		}
	}
}

// --- groupByUsername ---

func TestGroupByUsername_Nil(t *testing.T) {
	if g := groupByUsername(nil); len(g) != 0 {
		t.Errorf("expected 0 groups for nil input, got %d", len(g))
	}
}

func TestGroupByUsername_Empty(t *testing.T) {
	if g := groupByUsername([]Message{}); len(g) != 0 {
		t.Errorf("expected 0 groups for empty slice, got %d", len(g))
	}
}

func TestGroupByUsername_SingleMessage(t *testing.T) {
	groups := groupByUsername([]Message{{Content: "hi", Username: "Alice"}})
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Fatalf("expected 1 group of 1 message, got %d groups", len(groups))
	}
}

func TestGroupByUsername_ConsecutiveSameUser_MergesIntoOneGroup(t *testing.T) {
	msgs := []Message{
		{Content: "first", Username: "Alice"},
		{Content: "second", Username: "Alice"},
		{Content: "third", Username: "Alice"},
	}
	groups := groupByUsername(msgs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group for same user, got %d", len(groups))
	}
	if len(groups[0]) != 3 {
		t.Errorf("expected 3 messages in group, got %d", len(groups[0]))
	}
}

func TestGroupByUsername_AllDifferentUsers_OneGroupEach(t *testing.T) {
	msgs := []Message{
		{Content: "a", Username: "Alice"},
		{Content: "b", Username: "Bob"},
		{Content: "c", Username: "Carol"},
	}
	groups := groupByUsername(msgs)
	if len(groups) != 3 {
		t.Errorf("expected 3 groups for 3 different users, got %d", len(groups))
	}
}

func TestGroupByUsername_InterleavedUsers_SplitsCorrectly(t *testing.T) {
	// Alice, Bob, Alice — Bob breaks the consecutive run.
	msgs := []Message{
		{Content: "a1", Username: "Alice"},
		{Content: "b1", Username: "Bob"},
		{Content: "a2", Username: "Alice"},
	}
	groups := groupByUsername(msgs)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0][0].Username != "Alice" || groups[1][0].Username != "Bob" || groups[2][0].Username != "Alice" {
		t.Errorf("unexpected group order")
	}
}

func TestGroupByUsername_NoUsername_AllServerLogsMerge(t *testing.T) {
	msgs := []Message{
		{Content: "Server started"},
		{Content: "World saved"},
		{Content: "Player joined"},
	}
	groups := groupByUsername(msgs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group for server logs, got %d", len(groups))
	}
	if len(groups[0]) != 3 {
		t.Errorf("expected 3 messages in server log group, got %d", len(groups[0]))
	}
}

func TestGroupByUsername_MixedServerAndChat_SplitsOnChange(t *testing.T) {
	msgs := []Message{
		{Content: "Server started"},
		{Content: "hi", Username: "Alice"},
		{Content: "World saved"},
	}
	groups := groupByUsername(msgs)
	if len(groups) != 3 {
		t.Errorf("expected 3 groups (server, Alice, server), got %d", len(groups))
	}
}

// --- formatGroup ---

func TestFormatGroup_NoUsername_WrapsInCodeBlock(t *testing.T) {
	group := []Message{{Content: "World saved"}, {Content: "Disk: 40%"}}
	chunks := formatGroup(group, 1900)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	if !strings.HasPrefix(chunks[0], "```") {
		t.Errorf("server output should be wrapped in code block, got: %q", chunks[0])
	}
}

func TestFormatGroup_SystemUsername_WrapsInCodeBlock(t *testing.T) {
	group := []Message{{Content: "Blood Moon is rising...", Username: SystemUsername}}
	chunks := formatGroup(group, 1900)
	if !strings.HasPrefix(chunks[0], "```") {
		t.Errorf("SystemUsername should produce a code block, got: %q", chunks[0])
	}
}

func TestFormatGroup_ChatUsername_PlainText(t *testing.T) {
	group := []Message{{Content: "hey everyone", Username: "Steve"}}
	chunks := formatGroup(group, 1900)
	if strings.HasPrefix(chunks[0], "```") {
		t.Errorf("chat messages should NOT be wrapped in a code block")
	}
	if chunks[0] != "hey everyone" {
		t.Errorf("expected content unchanged, got %q", chunks[0])
	}
}

func TestFormatGroup_MultipleMessages_JoinedByNewline(t *testing.T) {
	group := []Message{
		{Content: "line one", Username: "Alice"},
		{Content: "line two", Username: "Alice"},
	}
	chunks := formatGroup(group, 1900)
	if !strings.Contains(chunks[0], "\n") {
		t.Errorf("expected lines joined by newline, got: %q", chunks[0])
	}
}

func TestFormatGroup_OversizedContent_ProducesMultipleChunks(t *testing.T) {
	group := []Message{{Content: strings.Repeat("a", 50), Username: "Alice"}}
	chunks := formatGroup(group, 20)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks for oversized content, got %d", len(chunks))
	}
}

// --- parseRetryAfter ---

func TestParseRetryAfter_NilError_ReturnsZero(t *testing.T) {
	if d := parseRetryAfter(nil); d != 0 {
		t.Errorf("expected 0 for nil error, got %v", d)
	}
}

func TestParseRetryAfter_UnrelatedError_ReturnsZero(t *testing.T) {
	if d := parseRetryAfter(fmt.Errorf("connection refused")); d != 0 {
		t.Errorf("expected 0 for non-429 error, got %v", d)
	}
}

func TestParseRetryAfter_RateLimitError_ReturnsPositive(t *testing.T) {
	d := parseRetryAfter(fmt.Errorf("discord error: 429 Too Many Requests"))
	if d <= 0 {
		t.Errorf("expected positive retry-after for 429, got %v", d)
	}
}

func TestParseRetryAfter_500Error_ReturnsZero(t *testing.T) {
	if d := parseRetryAfter(fmt.Errorf("500 internal server error")); d != 0 {
		t.Errorf("expected 0 for 500 error, got %v", d)
	}
}

// --- Batcher in isolation ---
// These tests access the batcher goroutine directly via internal channels,
// which lets us verify batching logic without triggering Discord API calls.

func newBatcherOnly(flushInterval time.Duration, maxBatchLines int) *Sender {
	cfg := &SenderConfig{
		FlushInterval: flushInterval,
		MaxBatchLines: maxBatchLines,
	}
	cfg.applyDefaults()
	return &Sender{
		cfg:    cfg,
		inbox:  make(chan Message, 256),
		work:   make(chan []Message, 32),
		stopCh: make(chan struct{}),
	}
}

func TestBatcher_FlushesOnTimer(t *testing.T) {
	s := newBatcherOnly(30*time.Millisecond, 1000)

	var mu sync.Mutex
	var received int

	workDone := make(chan struct{})
	go func() {
		defer close(workDone)
		for batch := range s.work {
			mu.Lock()
			received += len(batch)
			mu.Unlock()
		}
	}()

	s.wg.Add(1)
	go s.batcher()

	s.inbox <- Message{Content: "ping"}
	s.inbox <- Message{Content: "pong"}

	time.Sleep(80 * time.Millisecond) // wait for timer to fire

	close(s.stopCh)
	s.wg.Wait()

	select {
	case <-workDone:
	case <-time.After(time.Second):
		t.Fatal("work channel not closed")
	}

	mu.Lock()
	defer mu.Unlock()
	if received < 2 {
		t.Errorf("expected at least 2 messages flushed via timer, got %d", received)
	}
}

func TestBatcher_EarlyFlushOnMaxBatchLines(t *testing.T) {
	const maxLines = 5
	s := newBatcherOnly(10*time.Second, maxLines) // long timer — batch size must be the trigger

	var mu sync.Mutex
	var received int

	workDone := make(chan struct{})
	go func() {
		defer close(workDone)
		for batch := range s.work {
			mu.Lock()
			received += len(batch)
			mu.Unlock()
		}
	}()

	s.wg.Add(1)
	go s.batcher()

	for i := range maxLines {
		s.inbox <- Message{Content: fmt.Sprintf("msg%d", i)}
	}

	time.Sleep(50 * time.Millisecond) // allow early flush to happen

	close(s.stopCh)
	s.wg.Wait()

	select {
	case <-workDone:
	case <-time.After(time.Second):
		t.Fatal("work channel not closed")
	}

	mu.Lock()
	defer mu.Unlock()
	if received < maxLines {
		t.Errorf("expected %d messages in early flush, got %d", maxLines, received)
	}
}

func TestBatcher_DrainOnStop_NoMessagesLost(t *testing.T) {
	const n = 20
	// Very long flush interval — only the drain path fires.
	s := newBatcherOnly(10*time.Second, 1000)

	var received int
	workDone := make(chan struct{})
	go func() {
		defer close(workDone)
		for batch := range s.work {
			received += len(batch)
		}
	}()

	s.wg.Add(1)
	go s.batcher()

	for i := range n {
		s.inbox <- Message{Content: fmt.Sprintf("msg%d", i)}
	}

	close(s.stopCh)
	s.wg.Wait()

	select {
	case <-workDone:
	case <-time.After(time.Second):
		t.Fatal("work channel not closed after batcher exited")
	}

	if received != n {
		t.Errorf("expected %d messages drained on stop, got %d", n, received)
	}
}

func TestBatcher_EmptyInbox_NothingDispatched(t *testing.T) {
	s := newBatcherOnly(20*time.Millisecond, 100)

	dispatches := 0
	workDone := make(chan struct{})
	go func() {
		defer close(workDone)
		for range s.work {
			dispatches++
		}
	}()

	s.wg.Add(1)
	go s.batcher()

	time.Sleep(100 * time.Millisecond) // let timer fire several times with no messages

	close(s.stopCh)
	s.wg.Wait()

	select {
	case <-workDone:
	case <-time.After(time.Second):
		t.Fatal("work channel not closed")
	}

	if dispatches != 0 {
		t.Errorf("expected no dispatches for empty inbox, got %d", dispatches)
	}
}

// --- Sender: full lifecycle (no delivery) ---

// newLifecycleSender creates a Sender that can Start/Stop safely.
// Workers will block waiting for work until Stop() closes the work channel —
// they never call doSend because no messages reach them in these tests.
func newLifecycleSender() *Sender {
	return &Sender{
		cfg: &SenderConfig{
			FlushInterval:    20 * time.Millisecond,
			MaxBatchLines:    15,
			Workers:          2,
			RateLimit:        5,
			RateWindow:       5 * time.Second,
			MaxRetries:       0,
			MaxMessageLength: 1900,
		},
		limiter: rate.NewLimiter(rate.Every(time.Second), 5),
		inbox:   make(chan Message, 256),
		work:    make(chan []Message, 8),
		stopCh:  make(chan struct{}),
	}
}

func TestSender_StartStop_EmptyInbox_NoDeadlock(t *testing.T) {
	s := newLifecycleSender()
	s.Start(context.Background())

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() deadlocked with empty inbox")
	}
}

func TestSender_Send_NonBlocking_WhenInboxFull(t *testing.T) {
	s := newLifecycleSender()
	// Do NOT start — inbox fills up without being drained.

	for range 256 {
		s.inbox <- Message{Content: "fill"}
	}

	returned := make(chan struct{})
	go func() {
		s.Send(Message{Content: "overflow"})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("Send() blocked when inbox was full — should drop the message")
	}
}

func TestSender_ConcurrentSend_RaceFree(t *testing.T) {
	// Run with: go test -race ./internal/discord/...
	// Only the batcher runs — no delivery attempted (workers blocked on empty work chan
	// because messages are held in inbox until stop triggers drain).
	s := newBatcherOnly(5*time.Millisecond, 100)

	// Drain work channel so batcher doesn't block.
	go func() {
		for range s.work {
		}
	}()

	s.wg.Add(1)
	go s.batcher()

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range 20 {
				select {
				case s.inbox <- Message{Content: fmt.Sprintf("g%d-m%d", id, i)}:
				default:
				}
			}
		}(g)
	}
	wg.Wait()

	close(s.stopCh)
	s.wg.Wait()
}

// ── formatGroup: code block overhead ─────────────────────────────────────────

// TestFormatGroup_CodeBlockOverhead_ContentFitsWhenUnderLimit verifies that
// the code block fences (``` \n ... \n ```) added to server output don't
// cause Discord's 2000-char limit to be exceeded when content is close to
// the limit. This is particularly important when maxLen is set to 1900 to
// leave headroom — the fence overhead is about 8 runes.
func TestFormatGroup_CodeBlockOverhead_ContentFitsWhenUnderLimit(t *testing.T) {
	// Content is 10 runes under the limit. Code block fences are ~8 runes.
	// The combined result should fit in a single chunk.
	content := strings.Repeat("x", 90)
	group := []Message{{Content: content}} // no username → gets code block
	chunks := formatGroup(group, 100)

	// The code block adds "```\n" (4) + "\n```" (4) = 8 extra runes.
	// 90 + 8 = 98 < 100, so it should be one chunk.
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk when content + fences fit within limit, got %d (content len=%d)",
			len(chunks), len([]rune(content)))
	}
}

func TestFormatGroup_CodeBlockOverhead_SplitsWhenContentPlusFencesExceedLimit(t *testing.T) {
	// Content is exactly at the limit. Adding fences pushes it over.
	// The result must be split — we should not produce invalid Discord messages.
	content := strings.Repeat("x", 100)
	group := []Message{{Content: content}} // no username → gets code block
	chunks := formatGroup(group, 100)

	// Verify all chunks are within the limit (including the fence characters).
	for i, chunk := range chunks {
		if len([]rune(chunk)) > 100 {
			t.Errorf("chunk %d exceeds limit: %d runes", i, len([]rune(chunk)))
		}
	}
}

// ── groupByUsername: SystemUsername boundary ──────────────────────────────────

func TestGroupByUsername_SystemUsername_ConsecutiveMerge(t *testing.T) {
	// Multiple consecutive SystemUsername messages (e.g. multiple console
	// events) should merge into one group and appear as a single code block.
	msgs := []Message{
		{Content: "World saved.", Username: SystemUsername},
		{Content: "Listening on port 7777", Username: SystemUsername},
	}
	groups := groupByUsername(msgs)
	if len(groups) != 1 {
		t.Errorf("consecutive SystemUsername messages should merge, got %d groups", len(groups))
	}
}

func TestGroupByUsername_SystemUsername_BetweenPlayerMessages_Splits(t *testing.T) {
	// A system message between two player messages should form its own group.
	msgs := []Message{
		{Content: "hi", Username: "Alice"},
		{Content: "World saved.", Username: SystemUsername},
		{Content: "hello", Username: "Alice"},
	}
	groups := groupByUsername(msgs)
	if len(groups) != 3 {
		t.Errorf("expected 3 groups (Alice, System, Alice), got %d", len(groups))
	}
	if groups[1][0].Username != SystemUsername {
		t.Errorf("middle group should be SystemUsername, got %q", groups[1][0].Username)
	}
}

// ── splitMessage: content integrity ──────────────────────────────────────────

func TestSplitMessage_Reassembly_ProducesOriginalString(t *testing.T) {
	// Joining all chunks must reproduce the original input exactly.
	// If any rune is duplicated or dropped, this fails.
	original := strings.Repeat("abcde", 80) // 400 runes
	parts := splitMessage(original, 60)

	reassembled := strings.Join(parts, "")
	if reassembled != original {
		t.Errorf("reassembled string differs from original (len original=%d, reassembled=%d)",
			len([]rune(original)), len([]rune(reassembled)))
	}
}
