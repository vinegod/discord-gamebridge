package bot

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/executor"
	"github.com/vinegod/discordgamebridge/internal/version"
)

// BotWrapper encapsulates the Discord client and handles slash command routing and execution.
type BotWrapper struct {
	Client     *bot.Client
	cfg        config.Config
	commandMap map[string]*config.CommandConfig
	executors  *executor.Registry
	ctx        context.Context
	reloadCh   chan struct{}
}

func NewBot(
	ctx context.Context,
	cfg config.Config, //nolint:gocritic //reason:copy value to new bot once
	reloadCh chan struct{},
	reg *executor.Registry,
) (*BotWrapper, error) {
	b := &BotWrapper{
		cfg:       cfg,
		executors: reg,
		ctx:       ctx,
		reloadCh:  reloadCh,
	}

	intents := gateway.IntentGuildMessages
	var opts []bot.ConfigOpt

	if cfg.Server.ChatTemplate != "" {
		intents |= gateway.IntentMessageContent
		opts = append(opts, bot.WithEventListenerFunc(b.onMessageCreate))
	}

	if len(cfg.Commands) > 0 {
		opts = append(opts, bot.WithEventListenerFunc(b.onApplicationCommand))
	}

	opts = append(opts, bot.WithGatewayConfigOpts(gateway.WithIntents(intents)))

	client, err := disgo.New(cfg.Bot.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	b.commandMap = make(map[string]*config.CommandConfig, len(cfg.Commands))
	for i := range cfg.Commands {
		b.commandMap[cfg.Commands[i].Name] = &cfg.Commands[i]
	}

	b.Client = client
	return b, nil
}

func (b *BotWrapper) onApplicationCommand(event *events.ApplicationCommandInteractionCreate) {
	commandName := event.Data.CommandName()
	cmdCfg, ok := b.commandMap[commandName]
	if !ok {
		slog.Warn("command not found", "name", commandName)
		return
	}

	if !b.hasPermission(event, cmdCfg.Permissions) {
		_ = event.CreateMessage(discord.MessageCreate{
			Content: "**Permission Denied:** You do not have the required roles to run this command.",
			Flags:   discord.MessageFlagEphemeral,
		})
		return
	}

	switch cmdCfg.Type {
	case config.CommandTypeExecutor, config.CommandTypeScript:
		b.handleExecutorCommand(b.ctx, event, cmdCfg)
	case config.CommandTypeInternal:
		b.handleInternalCommand(event, cmdCfg)
	default:
		slog.Error("unknown command type", "name", cmdCfg.Name, "type", cmdCfg.Type)
	}
}

func (b *BotWrapper) hasPermission(
	event *events.ApplicationCommandInteractionCreate,
	perms config.PermissionConfig,
) bool {
	if len(perms.AllowedRoles) == 0 && len(perms.AllowedUsers) == 0 {
		return true
	}

	var roleIDs []string
	if member := event.Member(); member != nil {
		roleIDs = make([]string, len(member.RoleIDs))
		for idx, id := range member.RoleIDs {
			roleIDs[idx] = id.String()
		}
	}
	return checkPermission(event.User().ID.String(), roleIDs, perms)
}

func checkPermission(userID string, memberRoleIDs []string, perms config.PermissionConfig) bool {
	if slices.Contains(perms.AllowedUsers, userID) {
		return true
	}

	for _, allowedRole := range perms.AllowedRoles {
		if allowedRole == "@everyone" {
			return true
		}
		if slices.Contains(memberRoleIDs, allowedRole) {
			return true
		}
	}

	return false
}

// handleExecutorCommand is the single dispatch point for tmux, rcon, and script types.
func (b *BotWrapper) handleExecutorCommand(
	ctx context.Context,
	event *events.ApplicationCommandInteractionCreate,
	cmdCfg *config.CommandConfig,
) {
	ex, err := b.executors.Get(cmdCfg.ExecutorName)
	if err != nil {
		replyEphemeral(event, fmt.Sprintf("Configuration error: %v", err))
		return
	}

	data := event.SlashCommandInteractionData()
	command, args, deferred := buildExecutorInput(cmdCfg, &data, event)

	ctx, cancel := context.WithTimeout(ctx, cmdCfg.CommandTimeout)
	defer cancel()

	output, err := ex.Send(ctx, command, args...)

	if deferred {
		replyDeferred(b.Client, event, output, err)
	} else {
		replyImmediate(event, cmdCfg.Name, output, err)
	}
}

func buildExecutorInput(
	cmdCfg *config.CommandConfig,
	data *discord.SlashCommandInteractionData,
	event *events.ApplicationCommandInteractionCreate,
) (command string, args []string, deferred bool) {
	if cmdCfg.Type == config.CommandTypeScript {
		args = append([]string{}, cmdCfg.StaticArgs...)
		for _, arg := range cmdCfg.Arguments {
			if val, ok := data.OptString(arg.Name); ok {
				args = append(args, val)
			} else if val, ok := data.OptBool(arg.Name); ok && val {
				args = append(args, "--"+arg.Name)
			}
		}
		_ = event.DeferCreateMessage(false)
		return cmdCfg.ScriptPath, args, true
	}
	return buildCommand(cmdCfg.Template, cmdCfg.Arguments, data), nil, false
}

// replyDeferred updates a previously deferred interaction with script output.
func replyDeferred(client *bot.Client, event *events.ApplicationCommandInteractionCreate, output string, err error) {
	var response string
	if err != nil {
		response = fmt.Sprintf("Script Failed: %v\n```text\n%s\n```", err, output)
	} else {
		response = fmt.Sprintf("Script Output:\n```text\n%s\n```", output)
	}
	response = truncateResponse(response)
	_, _ = client.Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
		Content: &response,
	})
}

// replyImmediate responds to a tmux or rcon command.
func replyImmediate(event *events.ApplicationCommandInteractionCreate, cmdName, output string, err error) {
	if err != nil {
		replyEphemeral(event, fmt.Sprintf("Failed to execute command: %v", err))
		return
	}
	var response string
	if strings.TrimSpace(output) != "" {
		response = truncateResponse(fmt.Sprintf("```\n%s\n```", output))
	} else {
		response = fmt.Sprintf("✅ `%s` executed.", cmdName)
	}
	if err := event.CreateMessage(discord.MessageCreate{Content: response}); err != nil {
		slog.Error("failed to respond to user", "command", cmdName, "error", err)
	}
}

// replyEphemeral sends a message visible only to the invoking user.
func replyEphemeral(event *events.ApplicationCommandInteractionCreate, content string) {
	if err := event.CreateMessage(discord.MessageCreate{
		Content: content,
		Flags:   discord.MessageFlagEphemeral,
	}); err != nil {
		slog.Error("failed to send ephemeral reply", "error", err)
	}
}

// truncateResponse caps a response at 1950 runes, appending a truncation notice if needed.
func truncateResponse(s string) string {
	runes := []rune(s)
	if len(runes) <= 1950 {
		return s
	}
	return string(runes[:1950]) + "\n...[Truncated]```"
}

// SyncCommands registers the configured commands with the Discord API globally.
func (b *BotWrapper) SyncCommands() error {
	if len(b.cfg.Commands) == 0 {
		slog.Info("no commands configured, skipping command sync")
		return nil
	}

	appCommands := make([]discord.ApplicationCommandCreate, len(b.cfg.Commands))
	for idx := range b.cfg.Commands {
		var options []discord.ApplicationCommandOption

		for _, arg := range b.cfg.Commands[idx].Arguments {
			if arg.Type == config.VariableTypeBool {
				options = append(options, discord.ApplicationCommandOptionBool{
					Name:        arg.Name,
					Description: arg.Description,
					Required:    arg.Required,
				})
			} else {
				options = append(options, discord.ApplicationCommandOptionString{
					Name:        arg.Name,
					Description: arg.Description,
					Required:    arg.Required,
				})
			}
		}

		appCommands[idx] = discord.SlashCommandCreate{
			Name:        b.cfg.Commands[idx].Name,
			Description: b.cfg.Commands[idx].Description,
			Options:     options,
		}
	}

	slog.Info("syncing commands to Discord API", "count", len(appCommands))
	if _, err := b.Client.Rest.SetGlobalCommands(b.Client.ApplicationID, appCommands); err != nil {
		return fmt.Errorf("failed to set global commands: %w", err)
	}

	return nil
}

func (b *BotWrapper) handleInternalCommand(
	event *events.ApplicationCommandInteractionCreate,
	cmdCfg *config.CommandConfig,
) {
	switch cmdCfg.Name {
	case "reload":
		b.executeReload(event)
	case "version":
		b.executeVersion(event)
	case "ping":
		b.executePing(event)
	default:
		slog.Warn("unhandled internal command", "command", cmdCfg.Name)
		_ = event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Unknown internal command: `%s`", cmdCfg.Name),
		})
	}
}

func (b *BotWrapper) executeReload(event *events.ApplicationCommandInteractionCreate) {
	_ = event.CreateMessage(discord.MessageCreate{
		Content: "Reloading configuration and restarting services...",
	})
	select {
	case b.reloadCh <- struct{}{}:
	default:
		slog.Debug("ignoring duplicate reload request")
	}
}

func (b *BotWrapper) executeVersion(event *events.ApplicationCommandInteractionCreate) {
	_ = event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("discord-gamebridge version: `%s`", version.Version),
	})
}

func (b *BotWrapper) executePing(event *events.ApplicationCommandInteractionCreate) {
	_ = event.CreateMessage(discord.MessageCreate{Content: "Pong! Bot is operational."})
}

// buildCommand extracts argument values from the interaction.
func buildCommand(tmpl string, args []config.ArgumentConfig, data *discord.SlashCommandInteractionData) string {
	values := make(map[string]string, len(args))
	for _, arg := range args {
		if val, ok := data.OptString(arg.Name); ok {
			values[arg.Name] = val
		}
	}
	return config.SubstituteTemplate(tmpl, values)
}
