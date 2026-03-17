package server

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

// StartTailer begins tailing the server's log file and routes parsed
// lines to the appropriate Discord channel via the provided Sender.
func StartTailer(ctx context.Context, serverCfg *config.ServerConfig, sender *discord.Sender) error {
	t, err := tail.TailFile(serverCfg.LogFilePath, tail.Config{
		Follow:   true,
		ReOpen:   true,
		Poll:     true,
		Location: &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd},
		Logger:   tail.DiscardingLogger,
	})
	if err != nil {
		return fmt.Errorf("failed to tail log file %s: %w", serverCfg.LogFilePath, err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Info("[tailer] stopping", "file", serverCfg.LogFilePath)
				err := t.Stop()
				if err != nil {
					slog.Error("Failed to stop tailer.", "Error", err)
				}
				return

			case line, ok := <-t.Lines:
				if !ok {
					slog.Error("[tailer] tail channel closed",
						"file", serverCfg.LogFilePath,
						"reason", t.Err(),
					)
					return
				}
				if line == nil || line.Err != nil {
					continue
				}
				processLogLine(line.Text, serverCfg, sender)
			}
		}
	}()

	return nil
}

// processLogLine matches a single log line against compiled regexes and routes it to the sender.
func processLogLine(line string, cfg *config.ServerConfig, sender *discord.Sender) {
	cleanLine := strings.TrimSpace(line)
	if cleanLine == "" {
		return
	}

	if cfg.CompiledIgnore != nil && cfg.CompiledIgnore.MatchString(cleanLine) {
		return
	}

	// 1. In-game chat: <PlayerName> message
	if cfg.CompiledChat != nil {
		if groups := extractGroups(cfg.CompiledChat, cleanLine); groups != nil {
			player := strings.TrimSpace(groups["player"])
			message := strings.TrimSpace(groups["message"])

			sender.Send(discord.Message{
				Content:  message,
				Username: player,
			})
			return
		}
	}

	// 2. Player join
	if cfg.CompiledJoin != nil {
		if groups := extractGroups(cfg.CompiledJoin, cleanLine); groups != nil {
			player := strings.TrimSpace(groups["player"])
			sender.Send(discord.Message{
				Content:  fmt.Sprintf("🟢 **%s** joined the server.", player),
				Username: "Server",
			})
			return
		}
	}

	// 3. Player leave
	if cfg.CompiledLeave != nil {
		if groups := extractGroups(cfg.CompiledLeave, cleanLine); groups != nil {
			player := strings.TrimSpace(groups["player"])
			sender.Send(discord.Message{
				Content:  fmt.Sprintf("🔴 **%s** left the server.", player),
				Username: "Server",
			})
			return
		}
	}

	// 4. Game Events
	if cfg.CompiledEvents != nil && cfg.CompiledEvents.MatchString(cleanLine) {
		sender.Send(discord.Message{
			Content:  cleanLine,
			Username: discord.SystemUsername,
		})
		return
	}

	// 5. Other console logs
	if cfg.CompiledConsole != nil && cfg.CompiledConsole.MatchString(cleanLine) {
		sender.Send(discord.Message{
			Content:  cleanLine,
			Username: discord.SystemUsername,
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
