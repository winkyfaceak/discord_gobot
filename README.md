# Discord Go Bot

A Docker-ready Discord slash-command bot written in Go, with useful starter
commands, weather forecasts, and an interactive Deadlock player statistics
browser, plus live Valve/Steam server scoreboards.

## Features

- Native Discord slash commands with public or private responses where useful.
- Weather forecasts from [wttr.in](https://wttr.in), with selectable units and
  forecast detail levels.
- Interactive Deadlock player lookup powered by
  [Deadlock API](https://deadlock-api.com/), including overview statistics,
  predicted rank, recent matches, current-game status, and build insights.
- Image-backed Deadlock dossier cards with button navigation across overview,
  rank, recent matches, current-game status, builds, and full snapshots,
  including official hero, rank, and item artwork when exposed by the API.
- Paginated Deadlock recent matches and per-user interaction ownership.
- Live Valve server scoreboard sessions rendered as industrial-style cards,
  with automatic refresh, roster paging, and owner-controlled closure.
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
| `/server-stats` | Open a live Source server scoreboard session. | `address` |

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

Interactive Deadlock responses render each view as a dark dossier-style PNG
card, with Discord buttons to change pages or refresh the data. Available
Deadlock API artwork is embedded into the card for hero portraits, rank
badges, and item rows; missing artwork falls back to the text layout. Set
`interactive:false image:true` to render a single card without navigation.

Example commands:

```text
/weather location:Dublin view:today units:m private:true
/deadlock-statistics account:playername view:overview recent-count:10
/deadlock-statistics account:playername interactive:false image:true
/server-stats address:203.0.113.10:27015
```

### Live Server Scoreboards

`/server-stats` accepts a public Source query endpoint in `IP:port` form, such
as `203.0.113.10:27015` or `[2001:db8::10]:27015`. Hostnames and private or
local-network addresses are rejected.

The command posts a public scoreboard card and updates it every 30 seconds for
up to 15 minutes. The user who opened the session can browse roster pages,
request an immediate refresh, or press **Close Session**. Starting a new
session replaces that user's earlier live scoreboard.

Scoreboards use standard Valve A2S query data: server/game name, map, player
counts, server flags, latency, and visible roster rows with player name, score,
and connected duration. Some servers do not expose individual player rows; in
that case the card still displays the reported player totals and server status.

## Configuration

| Variable | Required | Purpose |
| --- | --- | --- |
| `DISCORD_TOKEN` | Yes | Raw Discord bot token from the Developer Portal. Do not include the `Bot ` prefix. |
| `GUILD_ID` | No | Registers commands only in one server for faster development iteration. If unset, commands are global. |
| `APP_ID` | No | Discord application/client ID. If unset, the bot discovers it after connecting. |

The bot opens outbound Discord/web requests and outbound UDP queries for
`/server-stats`; it does not listen on a network port.

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
cards and live server scoreboards.

## Local Development

Requirements:

- Go 1.26 or newer.
- `curl` to use `/weather`.
- ImageMagick with the `magick` executable to use interactive Deadlock and live
  server scoreboard cards.
- Outbound UDP access to the public Source query endpoints used with
  `/server-stats`.

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
|-- internal/serverstats       # Valve A2S querying and live scoreboard cards
|-- internal/weather           # wttr.in integration
|-- Dockerfile
`-- compose.yaml
```

## External Services

- Discord Gateway and REST API for command registration and interactions.
- [wttr.in](https://wttr.in) for `/weather` results.
- [Deadlock API](https://deadlock-api.com/) for player, match, rank, build, and
  game-status data.
- Public Source-compatible game servers queried via Valve A2S UDP packets for
  `/server-stats` sessions.
