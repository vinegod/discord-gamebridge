package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/nxadm/tail"
	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/discord"
)

// StartTailer begins tailing the server's log file and routes parsed
// lines to the appropriate Discord channel via the provided sender.
func StartTailer(ctx context.Context, serverCfg *config.ServerConfig, sender discord.MessageSender) error {
	t, err := openTail(serverCfg.LogFilePath)
	if err != nil {
		return err
	}

	go runTailer(ctx, t, serverCfg, sender)

	return nil
}

func runTailer(ctx context.Context, t *tail.Tail, serverCfg *config.ServerConfig, sender discord.MessageSender) {
	lines := t.Lines
	for {
		select {
		case <-ctx.Done():
			slog.Info("[tailer] stopping", "file", serverCfg.LogFilePath)
			if err := t.Stop(); err != nil {
				slog.Error("[tailer] stop failed", "error", err)
			}
			return
		case line, ok := <-lines:
			if !ok {
				t, lines = reopenTail(t, serverCfg.LogFilePath)
				if t == nil {
					return
				}
				continue
			}
			if line == nil || line.Err != nil {
				continue
			}
			processLogLine(line.Text, serverCfg, sender)
		}
	}
}

func reopenTail(old *tail.Tail, path string) (t *tail.Tail, lines chan *tail.Line) {
	slog.Warn("[tailer] channel closed unexpectedly, reopening", "file", path, "reason", old.Err())
	_ = old.Stop()
	newT, err := openTail(path)
	if err != nil {
		slog.Error("[tailer] reopen failed, stopping", "file", path, "error", err)
		return nil, nil
	}
	return newT, newT.Lines
}

func openTail(path string) (*tail.Tail, error) {
	t, err := tail.TailFile(path, tail.Config{
		Follow:   true,
		ReOpen:   true,
		Poll:     true,
		Location: &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd},
		Logger:   tail.DiscardingLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to tail log file %s: %w", path, err)
	}
	return t, nil
}

// processLogLine evaluates the log rules in order and forwards the first
// matching line to the sender. Rules with ignore:true drop the line silently.
// All template variables ({{.line}}, {{.groupname}}) are expanded before
// sending. The rule's channel is stamped onto Message.Target so the sender
// can route to the correct Discord channel.
func processLogLine(line string, cfg *config.ServerConfig, sender discord.MessageSender) {
	cleanLine := strings.TrimSpace(line)
	if cleanLine == "" {
		return
	}

	for i := range cfg.LogRules {
		rule := &cfg.LogRules[i]
		if rule.Compiled == nil || !rule.Compiled.MatchString(cleanLine) {
			continue
		}
		if rule.Ignore {
			return
		}

		groups := config.ExtractGroups(rule.Compiled, cleanLine)
		if groups == nil {
			groups = make(map[string]string)
		}
		groups["line"] = cleanLine

		if sender == nil {
			return
		}

		sender.Send(discord.Message{
			Content:  config.SubstituteTemplate(rule.Message, groups),
			Username: config.SubstituteTemplate(rule.Username, groups),
			Target:   string(rule.Channel),
		})
		return
	}
}
