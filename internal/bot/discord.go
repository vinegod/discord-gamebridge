package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/vinegod/discordgamebridge/internal/config"
	"github.com/vinegod/discordgamebridge/internal/executor"
)

// BotWrapper encapsulates the Discord client and handles slash command routing and execution.
type BotWrapper struct {
	Client     *bot.Client
	Config     config.Config
	commandMap map[string]*config.CommandConfig
	ctx        context.Context
	reloadCh   chan struct{}
}

func NewBot(ctx context.Context, cfg config.Config, reloadCh chan struct{}) (*BotWrapper, error) { //nolint:gocritic // reason: config is intentionally passed by value
	b := &BotWrapper{Config: cfg, ctx: ctx, reloadCh: reloadCh}
	client, err := disgo.New(cfg.Bot.Token,
		bot.WithGatewayConfigOpts(gateway.WithIntents(
			gateway.IntentGuildMessages,
			gateway.IntentMessageContent,
		)),
		bot.WithEventListenerFunc(b.onMessageCreate),
		bot.WithEventListenerFunc(b.onApplicationCommand),
	)
	if err != nil {
		return nil, err
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
		slog.Warn("Command not found", "name", commandName)
		return
	}

	if !b.hasPermission(event, cmdCfg) {
		_ = event.CreateMessage(discord.MessageCreate{
			Content: "**Permission Denied:** You do not have the required roles to run this command.",
			Flags:   discord.MessageFlagEphemeral,
		})
		return
	}

	switch cmdCfg.Type {
	case config.CommandTypeTmux:
		b.handleTmuxCommand(b.ctx, event, cmdCfg)
	case config.CommandTypeScript:
		b.handleScriptCommand(b.ctx, event, cmdCfg)
	default:
		slog.Error("Unknown command type.", "Name", cmdCfg.Name, "Type", cmdCfg.Type)
	}
}

// hasPermission checks User IDs and Role IDs against the config
func (b *BotWrapper) hasPermission(event *events.ApplicationCommandInteractionCreate, cmdCfg *config.CommandConfig) bool {
	userID := event.User().ID.String()
	for _, allowedUser := range cmdCfg.Permissions.AllowedUsers {
		if userID == allowedUser {
			return true
		}
	}

	if event.Member() != nil {
		for _, userRoleID := range event.Member().RoleIDs {
			for _, allowedRole := range cmdCfg.Permissions.AllowedRoles {
				if userRoleID.String() == allowedRole {
					return true
				}
			}
		}
	}

	return false
}

// handleTmuxCommand formats and sends a command directly to the target tmux pane.
func (b *BotWrapper) handleTmuxCommand(ctx context.Context, event *events.ApplicationCommandInteractionCreate, cmdCfg *config.CommandConfig) {
	data := event.SlashCommandInteractionData()
	finalCmd := cmdCfg.Template

	for _, arg := range cmdCfg.Arguments {
		placeholder := "{{." + arg.Name + "}}"
		if val, ok := data.OptString(arg.Name); ok {
			finalCmd = strings.ReplaceAll(finalCmd, placeholder, val)
		} else {
			// Remove unfilled optional placeholders
			finalCmd = strings.ReplaceAll(finalCmd, placeholder, "")
		}
	}

	ctx, cancel := context.WithTimeout(ctx, cmdCfg.CommandTimeout)
	defer cancel()

	err := executor.SendCommand(ctx, b.Config.Server.TmuxSession, b.Config.Server.TmuxWindow, b.Config.Server.TmuxPane, finalCmd)
	if err != nil {
		if err := event.CreateMessage(discord.MessageCreate{Content: "Failed to execute command: " + err.Error()}); err != nil {
			slog.Error("Failed to respond to user.", "Command", cmdCfg.Name, "error", err.Error())
		}
		return
	}

	if err := event.CreateMessage(discord.MessageCreate{Content: "Command executed successfully in Tmux!"}); err != nil {
		slog.Error("Failed to respond to user.", "Command", cmdCfg.Name, "error", err.Error())
	}
}

// handleScriptCommand parses arguments and triggers local shell scripts, responding with the output.
func (b *BotWrapper) handleScriptCommand(ctx context.Context, event *events.ApplicationCommandInteractionCreate, cmdCfg *config.CommandConfig) {
	_ = event.DeferCreateMessage(false)

	args := append([]string{}, cmdCfg.StaticArgs...)
	data := event.SlashCommandInteractionData()

	for _, argConfig := range cmdCfg.Arguments {
		if val, ok := data.OptString(argConfig.Name); ok {
			args = append(args, val)
		} else if valBool, ok := data.OptBool(argConfig.Name); ok {
			if valBool {
				args = append(args, "--"+argConfig.Name)
			}
		}
	}

	// Pass the AllowedScriptDir from the bot's configuration
	ctx, cancel := context.WithTimeout(ctx, cmdCfg.CommandTimeout)
	defer cancel()

	output, err := executor.RunScript(ctx, cmdCfg.ScriptPath, b.Config.Bot.AllowedScriptDir, args)

	response := fmt.Sprintf("Script Output:\n```text\n%s\n```", output)
	if err != nil {
		response = fmt.Sprintf("Script Failed: \n```text\n%v\n```", err)
	}

	runes := []rune(response)
	if len(runes) > 1950 {
		response = string(runes[:1950]) + "\n...[Output Truncated]```"
	}

	_, _ = b.Client.Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
		Content: &response,
	})
}

// SyncCommands registers the configured commands with the Discord API globally.
func (b *BotWrapper) SyncCommands() error {
	appCommands := make([]discord.ApplicationCommandCreate, len(b.Config.Commands))

	for idx := range b.Config.Commands {
		var options []discord.ApplicationCommandOption

		for _, arg := range b.Config.Commands[idx].Arguments {
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
			Name:        b.Config.Commands[idx].Name,
			Description: b.Config.Commands[idx].Description,
			Options:     options,
		}
	}

	slog.Info("syncing commands to Discord API", "count", len(appCommands))
	_, err := b.Client.Rest.SetGlobalCommands(b.Client.ApplicationID, appCommands)

	return err
}
