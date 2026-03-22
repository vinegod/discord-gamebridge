package bot

import (
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	snowflake "github.com/disgoorg/snowflake/v2"
)

// ptr is a convenience helper for *string fields.
func ptr(s string) *string { return &s }

// makeUser builds a discord.User with the given ID, username, and optional GlobalName.
func makeUser(id uint64, username string, globalName *string) discord.User {
	return discord.User{
		ID:         snowflake.ID(id),
		Username:   username,
		GlobalName: globalName,
	}
}

// --- getSafeName ---

func TestGetSafeName_UsesGlobalName_WhenSet(t *testing.T) {
	user := makeUser(1, "vinegod", ptr("Vinegod"))
	if name := getSafeName(&user); name != "Vinegod" {
		t.Errorf("expected GlobalName 'Vinegod', got %q", name)
	}
}

func TestGetSafeName_FallsBackToUsername_WhenGlobalNameNil(t *testing.T) {
	user := makeUser(1, "vinegod", nil)
	if name := getSafeName(&user); name != "vinegod" {
		t.Errorf("expected Username 'vinegod', got %q", name)
	}
}

func TestGetSafeName_FallsBackToUsername_WhenGlobalNameEmpty(t *testing.T) {
	user := makeUser(1, "vinegod", ptr(""))
	if name := getSafeName(&user); name != "vinegod" {
		t.Errorf("expected Username 'vinegod' for empty GlobalName, got %q", name)
	}
}

func TestGetSafeName_StripsEmoji_FromGlobalName(t *testing.T) {
	user := makeUser(1, "vinegod", ptr("Vinegod 🎮"))
	name := getSafeName(&user)
	if strings.Contains(name, "🎮") {
		t.Errorf("emoji should be stripped, got %q", name)
	}
	if !strings.Contains(name, "Vinegod") {
		t.Errorf("letters should be preserved, got %q", name)
	}
}

func TestGetSafeName_EmojiOnlyName_FallsBackToID(t *testing.T) {
	user := makeUser(123456789, "vinegod", ptr("🎮🎯🎲"))
	name := getSafeName(&user)
	if name != "123456789" {
		t.Errorf("emoji-only name should fall back to user ID, got %q", name)
	}
}

func TestGetSafeName_WhitespaceOnlyName_FallsBackToID(t *testing.T) {
	user := makeUser(123456789, "vinegod", ptr("   "))
	name := getSafeName(&user)
	if name != "123456789" {
		t.Errorf("whitespace-only name should fall back to user ID, got %q", name)
	}
}

func TestGetSafeName_PreservesLettersDigitsSpacesPunctuation(t *testing.T) {
	user := makeUser(1, "x", ptr("Player_One.2"))
	name := getSafeName(&user)
	// underscore is punctuation, dot is punctuation
	if name != "Player_One.2" {
		t.Errorf("expected 'Player_One.2', got %q", name)
	}
}

func TestGetSafeName_TrimsLeadingTrailingSpace(t *testing.T) {
	user := makeUser(1, "x", ptr("  Alice  "))
	name := getSafeName(&user)
	if name != "Alice" {
		t.Errorf("expected trimmed name 'Alice', got %q", name)
	}
}

func TestGetSafeName_UsernameAlsoEmojiOnly_FallsBackToID(t *testing.T) {
	// Unlikely in practice, but the ID fallback must work for Username too.
	user := makeUser(999, "🎮", nil)
	name := getSafeName(&user)
	if name != "999" {
		t.Errorf("expected ID '999' when username is also emoji-only, got %q", name)
	}
}

// --- sanitizeChat ---

func TestSanitizeChat_PlainASCII_Unchanged(t *testing.T) {
	if out := sanitizeChat("hello world"); out != "hello world" {
		t.Errorf("plain ASCII should pass through unchanged, got %q", out)
	}
}

func TestSanitizeChat_Emoji_Preserved(t *testing.T) {
	// Emoji are printable (unicode.IsPrint returns true) and must not be stripped.
	if out := sanitizeChat("hi 🎮"); out != "hi 🎮" {
		t.Errorf("emoji should be preserved, got %q", out)
	}
}

func TestSanitizeChat_Newlines_Removed(t *testing.T) {
	out := sanitizeChat("line1\nline2\r\nline3")
	if strings.ContainsAny(out, "\n\r") {
		t.Errorf("newlines should be removed, got %q", out)
	}
}

func TestSanitizeChat_ControlCharacters_Removed(t *testing.T) {
	// Ctrl+C (0x03), Escape (0x1B), Bell (0x07)
	out := sanitizeChat("hello\x03world\x1Btest\x07")
	if strings.ContainsAny(out, "\x03\x1B\x07") {
		t.Errorf("control characters should be removed, got %q", out)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Errorf("non-control content should survive, got %q", out)
	}
}

func TestSanitizeChat_ZeroWidthSpace_Removed(t *testing.T) {
	// U+200B zero-width space is non-printable.
	out := sanitizeChat("hello\u200Bworld")
	if strings.Contains(out, "\u200B") {
		t.Errorf("zero-width space should be removed, got %q", out)
	}
}

func TestSanitizeChat_AllControlChars_ReturnsEmpty(t *testing.T) {
	out := sanitizeChat("\n\r\t\x00\x01\x1B")
	if out != "" {
		t.Errorf("all-control string should produce empty output, got %q", out)
	}
}

func TestSanitizeChat_UnicodeLetters_Preserved(t *testing.T) {
	// Non-ASCII letters from various scripts.
	input := "Привет мир Héllo"
	out := sanitizeChat(input)
	if out != input {
		t.Errorf("Unicode letters should pass through, got %q", out)
	}
}

func TestSanitizeChat_EmptyString(t *testing.T) {
	if out := sanitizeChat(""); out != "" {
		t.Errorf("empty input should produce empty output, got %q", out)
	}
}

// --- resolveMentions ---

func TestResolveMentions_NoMentions_ContentUnchanged(t *testing.T) {
	msg := discord.Message{Content: "hello everyone"}
	if out := resolveMentions(&msg); out != "hello everyone" {
		t.Errorf("message with no mentions should be unchanged, got %q", out)
	}
}

func TestResolveMentions_SingleMention_Replaced(t *testing.T) {
	user := makeUser(123, "alice", ptr("Alice"))
	msg := discord.Message{
		Content:  "hey <@123> how are you",
		Mentions: []discord.User{user},
	}
	out := resolveMentions(&msg)
	if strings.Contains(out, "<@123>") {
		t.Errorf("mention tag should be replaced, got %q", out)
	}
	if !strings.Contains(out, "@Alice") {
		t.Errorf("mention should be replaced with @Name, got %q", out)
	}
}

func TestResolveMentions_LegacyMentionFormat_Replaced(t *testing.T) {
	// Discord historically used <@!ID> for nickname mentions.
	user := makeUser(456, "bob", ptr("Bob"))
	msg := discord.Message{
		Content:  "hello <@!456>",
		Mentions: []discord.User{user},
	}
	out := resolveMentions(&msg)
	if strings.Contains(out, "<@!456>") {
		t.Errorf("legacy mention format should be replaced, got %q", out)
	}
}

func TestResolveMentions_MultipleMentions_AllReplaced(t *testing.T) {
	alice := makeUser(1, "alice", ptr("Alice"))
	bob := makeUser(2, "bob", ptr("Bob"))
	msg := discord.Message{
		Content:  "<@1> and <@2> both mentioned",
		Mentions: []discord.User{alice, bob},
	}
	out := resolveMentions(&msg)
	if strings.Contains(out, "<@1>") || strings.Contains(out, "<@2>") {
		t.Errorf("all mention tags should be replaced, got %q", out)
	}
	if !strings.Contains(out, "@Alice") || !strings.Contains(out, "@Bob") {
		t.Errorf("both names should appear, got %q", out)
	}
}

func TestResolveMentions_MentionRepeatedInMessage_BothReplaced(t *testing.T) {
	user := makeUser(99, "carol", ptr("Carol"))
	msg := discord.Message{
		Content:  "<@99> said hi and <@99> said bye",
		Mentions: []discord.User{user},
	}
	out := resolveMentions(&msg)
	if strings.Contains(out, "<@99>") {
		t.Errorf("repeated mention should be fully replaced, got %q", out)
	}
	if strings.Count(out, "@Carol") != 2 {
		t.Errorf("expected @Carol to appear twice, got %q", out)
	}
}

func TestResolveMentions_EmojiOnlyUsername_FallsBackToID(t *testing.T) {
	// User whose name sanitises to empty — resolveMentions must use ID instead.
	user := makeUser(777, "🎮", nil) // username is emoji-only
	msg := discord.Message{
		Content:  "hello <@777>",
		Mentions: []discord.User{user},
	}
	out := resolveMentions(&msg)
	// getSafeName returns the ID string when the name sanitises to empty.
	if strings.Contains(out, "<@777>") {
		t.Errorf("mention tag should be replaced even for emoji-only username, got %q", out)
	}
	if !strings.Contains(out, "@777") {
		t.Errorf("should fall back to @ID for emoji-only username, got %q", out)
	}
}
