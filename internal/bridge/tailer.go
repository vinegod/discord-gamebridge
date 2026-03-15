package bridge

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"github.com/nxadm/tail"
	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/discord"
)

// StartTailer begins tailing the bridge's log file and routes parsed
// lines to the appropriate Discord channel via the provided Sender.
//
// The Sender is responsible for rate limiting, batching, retrying, and
// choosing between webhook and bot-client delivery — the tailer does
// not need to know which transport is in use.
func StartTailer(ctx context.Context, bridgeCfg config.BridgeConfig, sender *discord.Sender) error {
	t, err := tail.TailFile(bridgeCfg.LogFilePath, tail.Config{
		Follow:   true,
		ReOpen:   true,
		Poll:     true,
		Location: &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd},
		Logger:   tail.DiscardingLogger,
	})
	if err != nil {
		return fmt.Errorf("failed to tail log file %s: %w", bridgeCfg.LogFilePath, err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Info("[bridge/tailer] stopping", "file", bridgeCfg.LogFilePath)
				t.Stop()
				return

			case line, ok := <-t.Lines:
				if !ok {
					slog.Error("[bridge/tailer] tail channel closed",
						"file", bridgeCfg.LogFilePath,
						"reason", t.Err(),
					)
					return
				}
				if line == nil || line.Err != nil {
					continue
				}
				processLogLine(line.Text, bridgeCfg, sender)
			}
		}
	}()

	return nil
}

// processLogLine parses a single log line against the bridge's compiled
// regexes and enqueues an appropriate Message on the Sender.
func processLogLine(line string, bridge config.BridgeConfig, sender *discord.Sender) {
	cleanLine := strings.TrimSpace(line)
	if cleanLine == "" {
		return
	}

	if bridge.CompiledIgnore != nil && bridge.CompiledIgnore.MatchString(cleanLine) {
		return
	}

	// 1. In-game chat: <PlayerName> message
	if bridge.CompiledChat != nil {
		if groups := extractGroups(bridge.CompiledChat, cleanLine); groups != nil {
			player := strings.TrimSpace(groups["player"])
			message := strings.TrimSpace(groups["message"])

			for _, ignored := range bridge.IgnoreChatNames {
				if strings.EqualFold(player, ignored) {
					return
				}
			}

			sender.Send(discord.Message{
				Content:  message,
				Username: player,
			})
			return
		}
	}

	// 2. Player join
	if bridge.CompiledJoin != nil {
		if groups := extractGroups(bridge.CompiledJoin, cleanLine); groups != nil {
			player := strings.TrimSpace(groups["player"])
			sender.Send(discord.Message{
				Content:  fmt.Sprintf("🟢 **%s** joined the server.", player),
				Username: "Server",
			})
			return
		}
	}

	// 3. Player leave
	if bridge.CompiledLeave != nil {
		if groups := extractGroups(bridge.CompiledLeave, cleanLine); groups != nil {
			player := strings.TrimSpace(groups["player"])
			sender.Send(discord.Message{
				Content:  fmt.Sprintf("🔴 **%s** left the server.", player),
				Username: "Server",
			})
			return
		}
	}

	// 4. Other console logs
	if bridge.CompiledConsole != nil && bridge.CompiledConsole.MatchString(cleanLine) {
		sender.Send(discord.Message{
			Content:  cleanLine,
			Username: "Server",
		})
		return
	}
}

// extractGroups maps a regex's named capture groups to a string map.
func extractGroups(re *regexp.Regexp, text string) map[string]string {
	match := re.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	results := make(map[string]string, len(re.SubexpNames()))
	for i, name := range re.SubexpNames() {
		if i != 0 && name != "" {
			results[name] = match[i]
		}
	}
	return results
}
