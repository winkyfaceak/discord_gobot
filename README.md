# Discord Go Bot

A Docker-ready Discord slash-command bot written in Go, with useful starter
commands, weather forecasts, and an interactive Deadlock player statistics
browser.

## Features

- Native Discord slash commands with public or private responses where useful.
- Weather forecasts from [wttr.in](https://wttr.in), with selectable units and
  forecast detail levels.
- Interactive Deadlock player lookup powered by
  [Deadlock API](https://deadlock-api.com/), including overview statistics,
  predicted rank, recent matches, current-game status, and build insights.
- Button-based navigation for Deadlock results, including paginated recent
  matches and per-user interaction ownership.
- Optional rendered PNG stat cards for Deadlock overviews using ImageMagick.
- Container-first deployment with Docker Compose support and graceful shutdown.

## Commands

| Command | Description | Notable Options |
| --- | --- | --- |
| `/ping` | Check that the bot is online. | None |
| `/hello` | Send a greeting. | `name` |
| `/echo` | Repeat text back in Discord. | `text`, `private` |
| `/add` | Add two integers. | `a`, `b` |
| `/weather` | Fetch a weather report for a place. | `location`, `view`, `units`, `private` |
| `/deadlock-statistics` | Browse a player's Deadlock performance and status. | `account`, `view`, `recent-count`, `rank-image`, `interactive`, `image`, `private` |

### Deadlock Views

The `/deadlock-statistics` command can open directly to:

| View | Contents |
| --- | --- |
| Overview | Match totals, win rate, averages, and top hero. |
| Rank image | Predicted rank information with optional badge art. |
| Recent games | Recent match results with button pagination. |
| Current game/status | Whether the selected player is in an active match. |
| Builds & items | Build insights for the player's top hero. |
| All snapshot | A broader combined snapshot of available details. |

Example commands:

```text
/weather location:Dublin view:today units:m private:true
/deadlock-statistics account:playername view:overview recent-count:10
/deadlock-statistics account:playername interactive:false image:true
```

## Configuration

| Variable | Required | Purpose |
| --- | --- | --- |
| `DISCORD_TOKEN` | Yes | Raw Discord bot token from the Developer Portal. Do not include the `Bot ` prefix. |
| `GUILD_ID` | No | Registers commands only in one server for faster development iteration. If unset, commands are global. |
| `APP_ID` | No | Discord application/client ID. If unset, the bot discovers it after connecting. |

The bot opens an outbound Discord websocket connection and does not listen on a
network port.

## Quick Start With Docker Compose

Set the Discord configuration in your shell:

```sh
export DISCORD_TOKEN="your_raw_bot_token_here"
export GUILD_ID="your_test_server_id_here"
export APP_ID="your_application_id_here"
```

Start the bot:

```sh
docker compose up -d --build
```

Check startup and command registration logs:

```sh
docker compose logs -f bot
```

Stop the bot:

```sh
docker compose down
```

## Docker Deployment

To run without Compose, build and launch the image directly:

```sh
docker build -t discord-gobot .

docker run -d \
  --name discord-gobot \
  --restart unless-stopped \
  -e DISCORD_TOKEN="your_raw_bot_token_here" \
  -e GUILD_ID="your_test_server_id_here" \
  -e APP_ID="your_application_id_here" \
  discord-gobot
```

The runtime image runs the bot as an unprivileged user and includes `curl` for
weather lookups plus ImageMagick SVG support and fonts for Deadlock statistics
cards.

## Local Development

Requirements:

- Go 1.26 or newer.
- `curl` to use `/weather`.
- ImageMagick with the `magick` executable to use generated Deadlock stat cards.

Run the bot locally:

```sh
export DISCORD_TOKEN="your_raw_bot_token_here"
export GUILD_ID="your_test_server_id_here"
go run .
```

Run the package checks:

```sh
go test ./...
```

## Project Layout

```text
.
|-- main.go                    # Application entrypoint
|-- internal/bot               # Discord connection and interaction routing
|-- internal/commands          # Slash commands and component handlers
|-- internal/config            # Environment-based configuration
|-- internal/deadlock          # Deadlock API client, summaries, and image cards
|-- internal/discordutil       # Discord response helpers
|-- internal/weather           # wttr.in integration
|-- Dockerfile
`-- compose.yaml
```

## External Services

- Discord Gateway and REST API for command registration and interactions.
- [wttr.in](https://wttr.in) for `/weather` results.
- [Deadlock API](https://deadlock-api.com/) for player, match, rank, build, and
  game-status data.
