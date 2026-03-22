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

- Two-Way Chat Routing: Reads server logs via regex parsing and forwards in-game chat to Discord webhooks. Forwards Discord messages back to the game console via tmux.
- Log Tailing & Filtering: Monitors server log files and filters output based on configurable regular expressions (e.g., player joins, leaves, console events).
- Remote Command Execution: Execute local shell scripts or tmux commands from Discord using slash commands.
- Role-Based Access Control: Restrict specific commands to designated Discord User IDs or Role IDs.
- Rate-Limited Dispatching: Batches outgoing Discord messages to comply with API rate limits and prevent spam.

## Installation
### Prerequisites

1. [Go](https://go.dev/) 1.25.0 or higher
2. A registered Discord Bot with a valid Token ([check discord developer portal](https://discord.com/developers))
3. [tmux](https://github.com/tmux/tmux/wiki) (for tmux command execution)

### Build Instructions

1. Clone the repository:
    Bash

```bash
git clone https://github.com/vinegod/discordgamebridge.git
cd discordgamebridge
```

2. Build the binary:

```bash
go build
```

## Usage

1. **Environment Setup**: Create a `.env` file in the root directory to store your Discord token and chat webhook:

```python
DISCORD_TOKEN=your_bot_token_here
DISCORD_WEBHOOK="https://discord.com/api/webhooks/xxx"
```

2. **Configuration:** Create a `config.yaml` file. Example:

```yaml
bot:
  token_env_var: "DISCORD_TOKEN"
  log_level: "info"
  allowed_script_dir: "/home/user/scripts"

server:
  tmux_session: "terraria_server"
  discord_chat_channel_id: "123456789012345678"
  chat_template: "say {{.player}} {{.reason}}"
  log_file_path: "/home/user/terraria_server/logs/server.log"

  regex_parsers:
    chat: '^(?P<player>[a-zA-Z0-9_]+): (?P<message>.*)$'
    ignore: '^<Server> .*$'

commands:
  - name: "start"
    description: "Boots the server"
    type: "script"
    script_path: "start.sh"
    permissions:
      allowed_users: ["YOUR_DISCORD_ID"]
```

Or check detailed example [config_example.yaml](./config_example.yaml)

3. **Run the application**:

```bash
./discordgamebridge -config=config_example.yaml
```

4. Force debug mode

In this mode force logger to use debug mode
```bash
./discordgamebridge -config=config_example.yaml -debug
```

5. Validate config

Run to validate input config for errors (runs in debug mode)

```bash
./discordgamebridge -config=config_example.yaml -validate
```

6. Show version

Run to see app version

```bash
./discordgamebridge -version
```

## Roadmap

The following features are planned for future implementation:

- [x] CLI support: Add possibility to pass different configuration and other parameters via CLI.
- [x] Config reload command: reload configuration without restarting bot.
- [ ] Docs: Add more documentation and examples.
- [ ] Better template support: current chat template is very limited, Go provides much better tools for it.
- [ ] Interactive Confirmations: Discord UI buttons to confirm or cancel commands.
- [ ] "Ready and go": Add more default scripts...
