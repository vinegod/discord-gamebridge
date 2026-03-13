package bot

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/vinegod/discordgamebridge/internal/executor"
)

// onMessageCreate handles Two-Way Chat (Discord -> Game)
func (b *BotWrapper) onMessageCreate(event *events.MessageCreate) {
	// Ignore bots (prevents endless feedback loops if the bot reads its own messages)
	if event.Message.Author.Bot {
		return
	}

	for _, bridge := range b.Config.Bridges {
		if bridge.Enabled && event.Message.ChannelID.String() == bridge.DiscordChatChannelID {

			// Resolve mentions and sanitize the raw input
			cleanText := resolveMentions(event.Message)
			safeMessage := sanitizeChat(cleanText)

			// Don't send empty messages (e.g., if the user only posted an image or newlines)
			if safeMessage == "" {
				return
			}

			// Get a safe representation of the Author's name
			authorName := getSafeName(event.Message.Author)

			gameCommand := bridge.ChatTemplate
			gameCommand = strings.ReplaceAll(gameCommand, "{{.user}}", authorName)
			gameCommand = strings.ReplaceAll(gameCommand, "{{.message}}", safeMessage)

			ctx, cancel := context.WithTimeout(b.ctx, bridge.ChatTimeout)
			defer cancel()

			// Send to the multiplexer
			err := executor.SendCommand(ctx, bridge.TmuxSession, bridge.TmuxWindow, bridge.TmuxPane, gameCommand)
			if err != nil {
				fmt.Errorf("Failed to send chat to tmux: %w", err)
			}
			return
		}
	}
}

// getSafeName removes emojis and unsupported symbols from a username.
// If the resulting name is completely empty, it falls back to the user's Discord ID.
func getSafeName(user discord.User) string {
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
func resolveMentions(msg discord.Message) string {
	content := msg.Content

	for _, user := range msg.Mentions {
		idTag1 := "<@" + user.ID.String() + ">"
		idTag2 := "<@!" + user.ID.String() + ">"

		safeName := getSafeName(user)
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
