package bot

import (
	"context"
	"strings"
	"unicode"

	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

// onMessageCreate processes Discord messages and forwards them to the game server via the configured chat executor.
func (b *BotWrapper) onMessageCreate(event *events.MessageCreate) {
	if event.Message.Author.Bot {
		return
	}

	if event.Message.ChannelID.String() != b.cfg.Server.DiscordChatChannelID {
		return
	}

	cleanText := resolveMentions(&event.Message)
	safeMessage := sanitizeChat(cleanText)
	if safeMessage == "" {
		return
	}

	const maxGameChatLength = 200
	if runes := []rune(safeMessage); len(runes) > maxGameChatLength {
		safeMessage = string(runes[:maxGameChatLength])
	}

	authorName := getSafeName(&event.Message.Author)

	gameCommand := b.cfg.Server.ChatTemplate
	gameCommand = strings.ReplaceAll(gameCommand, "{{.user}}", authorName)
	gameCommand = strings.ReplaceAll(gameCommand, "{{.message}}", safeMessage)

	ex, err := b.executors.Get(b.cfg.Server.ChatExecutor)
	if err != nil {
		slog.Error("chat executor not found", "executor", b.cfg.Server.ChatExecutor, "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(b.ctx, b.cfg.Server.ChatTimeout)
	defer cancel()

	if _, err := ex.Send(ctx, gameCommand); err != nil {
		slog.Error("failed to send chat to game", "executor", b.cfg.Server.ChatExecutor, "error", err)
	}
}

// getSafeName removes emojis and unsupported symbols from a username.
// Falls back to the user's Discord ID if the cleaned name is empty.
func getSafeName(user *discord.User) string {
	var name string
	if user.GlobalName != nil && *user.GlobalName != "" {
		name = *user.GlobalName
	} else {
		name = user.Username
	}

	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || unicode.IsPunct(r) {
			b.WriteRune(r)
		}
	}

	if safe := strings.TrimSpace(b.String()); safe != "" {
		return safe
	}

	return user.ID.String()
}

// resolveMentions replaces <@ID> with @Username (or @ID if unprintable).
func resolveMentions(msg *discord.Message) string {
	content := msg.Content

	for _, user := range msg.Mentions {
		idTag1 := "<@" + user.ID.String() + ">"
		idTag2 := "<@!" + user.ID.String() + ">"

		safeName := getSafeName(&user)
		nameTag := "@" + safeName

		content = strings.ReplaceAll(content, idTag1, nameTag)
		content = strings.ReplaceAll(content, idTag2, nameTag)
	}

	return content
}

// sanitizeChat removes control characters and non-printable runes to prevent tmux injection or malformed game console input.
func sanitizeChat(input string) string {
	var builder strings.Builder

	for _, ch := range input {
		if unicode.IsControl(ch) {
			continue
		}
		if !unicode.IsPrint(ch) {
			continue
		}
		builder.WriteRune(ch)
	}

	return builder.String()
}
