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

type BotWrapper struct {
	Client     *bot.Client
	Config     config.Config
	commandMap map[string]*config.CommandConfig
	ctx        context.Context
}

func NewBot(ctx context.Context, cfg config.Config) (*BotWrapper, error) { //nolint:gocritic // reason: config is intentionally passed by value
	b := &BotWrapper{Config: cfg, ctx: ctx}

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

// onApplicationCommand routes incoming slash commands
func (b *BotWrapper) onApplicationCommand(event *events.ApplicationCommandInteractionCreate) {
	commandName := event.Data.CommandName()

	cmdCfg, ok := b.commandMap[commandName]
	if !ok {
		slog.Warn("Command", commandName, " not found")
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

// handleTmuxCommand processes and routes commands to gotmux
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

// handleScriptCommand parses arguments and triggers local shell scripts
func (b *BotWrapper) handleScriptCommand(ctx context.Context, event *events.ApplicationCommandInteractionCreate, cmdCfg *config.CommandConfig) {
	_ = event.DeferCreateMessage(false)

	// Pre-load the static arguments from YAML
	args := append([]string{}, cmdCfg.StaticArgs...)

	// Append any dynamic arguments provided by the user in Discord
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
		response = fmt.Sprintf("Script Failed: %v\n```text\n%s\n```", err, output)
	}

	runes := []rune(response)
	if len(runes) > 1950 {
		response = string(runes[:1950]) + "\n...[Output Truncated]```"
	}

	_, _ = b.Client.Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
		Content: &response,
	})
}

// SyncCommands reads the YAML config and publishes the Slash Commands to the Discord API
func (b *BotWrapper) SyncCommands() error {
	appCommands := make([]discord.ApplicationCommandCreate, len(b.Config.Commands))

	for _, cmd := range b.Config.Commands { //nolint:gocritic // reason: TODO: refactor command structures
		var options []discord.ApplicationCommandOption

		// Convert YAML arguments into Discord UI options
		for _, arg := range cmd.Arguments {
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

		// Build the Discord command payload
		slashCmd := discord.SlashCommandCreate{
			Name:        cmd.Name,
			Description: cmd.Description,
			Options:     options,
		}

		appCommands = append(appCommands, slashCmd)
	}

	slog.Info("syncing commands to Discord API", "count", len(appCommands))

	// Push the commands to Discord globally
	_, err := b.Client.Rest.SetGlobalCommands(b.Client.ApplicationID, appCommands)
	return err
}
