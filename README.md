# Discord-gamebridge

> I wanted to create simple tool to manage my Terraria server and now we are here.

## Table of Contents
* [Table of Contents](#table-of-contents)
* [Description](#description)
* [Features](#features)
* [Installation](#installation)
  * [Prerequisites](#prerequisites)
  * [Build Instructions](#build-instructions)
* [Usage](#usage)
* [Roadmap](#roadmap)

## Description

Discord Gamebridge is a Go-based application that connects game servers (like Terraria or Minecraft) to Discord.
It provides two-way chat routing, log monitoring, and remote command execution via Discord slash commands.

## Features

- **Two-way chat**: Forwards in-game chat to Discord (via webhook or bot) and Discord messages back to the game console.
- **Config-driven log rules**: Ordered regex rules match log lines, expand named capture groups into templates, and route output to a chat or audit channel.
- **Remote command execution**: Run tmux commands, RCON commands, or local shell scripts from Discord slash commands.
- **Role-based access control**: Restrict commands to specific Discord user or role IDs.
- **Per-command cooldowns**: Limit how often each user can invoke a command.
- **Ephemeral responses**: Command output is sent only to the invoker by default; configurable per command.
- **Rate-limited dispatch**: Outgoing messages are batched and rate-limited to stay within Discord API limits.
- **Hot config reload**: Reload configuration without restarting the process via the `/reload` command.

## Installation
### Prerequisites

1. [Go](https://go.dev/) 1.25.0 or higher
2. A registered Discord Bot with a valid token ([Discord Developer Portal](https://discord.com/developers))
3. [tmux](https://github.com/tmux/tmux/wiki) — only needed when using the tmux executor

### Build Instructions

```bash
git clone https://github.com/vinegod/discordgamebridge.git
cd discordgamebridge
go build -o discord-gamebridge .
```

## Usage

1. **Environment variables**: Store secrets in a `.env` file or export them directly:

```bash
DISCORD_TOKEN=your_bot_token_here
DISCORD_WEBHOOK=https://discord.com/api/webhooks/xxx
```

2. **Configuration**: Create a `config.yaml` file. Minimal example:

```yaml
bot:
  token_env_var: "DISCORD_TOKEN"
  log_level: "info"

executors:
  tmux:
    type: "tmux"
    session: "terraria"
    window: 1
    pane: 0

server:
  discord_chat_channel_id: "123456789012345678"
  discord_webhook_env: "DISCORD_WEBHOOK"
  chat_executor: "tmux"
  chat_template: "say [Discord] {{.user}}: {{.message}}"
  log_file_path: "/var/log/terraria/server.log"

  log_rules:
    - name: ignore_server
      regex: '^<Server> .*$'
      ignore: true

    - name: chat
      regex: '^<(?P<player>[^>]+)> (?P<message>.*)$'
      username: "{{.player}}"
      message: "{{.message}}"
      channel: chat

    - name: join
      regex: '^(?P<player>[^\s]+) has joined\.$'
      username: "Server"
      message: "🟢 **{{.player}}** joined."
      channel: chat

commands:
  - name: "kick"
    description: "Kick a player"
    executor: "tmux"
    template: "kick {{.player}}"
    cooldown: 10s
    ephemeral_output: false
    permissions:
      allowed_roles: ["YOUR_ADMIN_ROLE_ID"]
    arguments:
      - name: "player"
        type: "string"
        description: "Exact in-game name"
        required: true
```

See [config_example.yaml](./config_example.yaml) for a full annotated reference.

3. **Run**:

```bash
./discord-gamebridge -config=config.yaml
```

4. **Flags**:

| Flag | Description |
|------|-------------|
| `-config <path>` | Path to config file (required) |
| `-validate` | Validate config and exit |
| `-debug` | Force debug log level |
| `-version` | Print version and exit |

## Roadmap

- [x] CLI flags: config path, validate, debug, version
- [x] Config hot reload via `/reload` command
- [x] RCON executor support
- [x] Script executor support
- [x] Config-driven log rules with named capture groups
- [x] Dual-channel routing (chat + audit/log channel)
- [x] Per-command cooldowns
- [x] Ephemeral command responses
- [ ] Interactive confirmations (Discord buttons for destructive commands)
- [ ] Better template support (Go template functions in message fields)
- [ ] More example scripts
