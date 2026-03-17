package bot

import (
	"context"
	"strings"
	"unicode"

	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/vinegod/discordgamebridge/internal/executor"
)

// // onMessageCreate processes Discord messages and forwards them to the game console via tmux.
func (b *BotWrapper) onMessageCreate(event *events.MessageCreate) {
	// Ignore bots (prevents endless feedback loops if the bot reads its own messages)
	if event.Message.Author.Bot {
		return
	}

	if event.Message.ChannelID.String() == b.Config.Server.DiscordChatChannelID {
		cleanText := resolveMentions(&event.Message)
		safeMessage := sanitizeChat(cleanText)

		if safeMessage == "" {
			return
		}

		const maxGameChatLength = 200
		runes := []rune(safeMessage)
		if len(runes) > maxGameChatLength {
			safeMessage = string(runes[:maxGameChatLength])
		}

		authorName := getSafeName(&event.Message.Author)

		gameCommand := b.Config.Server.ChatTemplate
		gameCommand = strings.ReplaceAll(gameCommand, "{{.user}}", authorName)
		gameCommand = strings.ReplaceAll(gameCommand, "{{.message}}", safeMessage)

		ctx, cancel := context.WithTimeout(b.ctx, b.Config.Server.ChatTimeout)
		defer cancel()

		err := executor.SendCommand(ctx, b.Config.Server.TmuxSession, b.Config.Server.TmuxWindow, b.Config.Server.TmuxPane, gameCommand)
		if err != nil {
			slog.Error("Failed to send chat to tmux.", "Error", err)
		}
		return
	}
}

// getSafeName removes emojis and unsupported symbols from a username.
// If the resulting name is completely empty, it falls back to the user's Discord ID.
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

// resolveMentions replaces <@ID> with @Username (or @ID if the username is unprintable)
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

// sanitizeChat strictly removes newlines and terminal control characters
func sanitizeChat(input string) string {
	var builder strings.Builder

	for _, ch := range input {
		// Drop ALL control characters (Newlines, Returns, Ctrl+C, Escape sequences)
		if unicode.IsControl(ch) {
			continue
		}

		// Drop non-printable characters (invisible zero-width spaces, etc.)
		if !unicode.IsPrint(ch) {
			continue
		}

		builder.WriteRune(ch)
	}

	return builder.String()
}
