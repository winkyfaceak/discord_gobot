# Discord Go Bot

A Discord slash-command bot written in Go, with weather forecasts, an
interactive Deadlock player statistics browser, live Valve/Steam server
scoreboards, and music from a Navidrome library played in voice channels.

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
- Music from your own Navidrome library in voice channels, with library
  autocomplete, a queue, and "now playing" cards.
- Home lab alerts from ntfy (service failures, disk space, temperatures)
  forwarded to the app owner's Discord DMs.
- Runs as a NixOS systemd service with graceful shutdown.

## Commands

| Command | Description | Notable Options |
| --- | --- | --- |
| `/ping` | Check that the bot is online. | None |
| `/weather` | Fetch a weather report for a place. | `location`, `view`, `units`, `private` |
| `/deadlock-statistics` | Browse a player's Deadlock performance and status. | `account`, `view`, `recent-count`, `rank-image`, `interactive`, `image`, `private` |
| `/server-stats` | Open a live Source server scoreboard session. | `address` |
| `/remind` | Ping someone (or yourself) in the channel later. Survives restarts. | `when` (`20m`, `1h30m`, `2 days`, `18:30`, `6pm`), `what`, `who` |
| `/timezone` | Set your time zone so others can convert the times you write. | `zone` (autocompletes) |
| Convert times (right-click a message → Apps) | Privately shows the times in a message ("on at 9pm") in your own time zone. | None |
| `/coinflip`, `/slots` | Bet fake coins on heads or tails (double or nothing) or a slot machine. | `call`, `bet` |
| `/daily`, `/balance`, `/leaderboard` | Free coins every 20 hours, balances, and the richest members. Coins are per server; everyone starts with 1,000. | `who` |
| `/music play` / `skip` / `queue` / `stop` | Play songs and albums from Navidrome in your voice channel. | `search` (autocompletes from the library) |

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
| `NAVIDROME_USER` | No | Navidrome login for `/music`. Without it, `/music` isn't registered. |
| `NAVIDROME_PASSWORD` | With `NAVIDROME_USER` | Password for that Navidrome login. |
| `NAVIDROME_URL` | No | Navidrome address. Defaults to `http://127.0.0.1:4533`. |
| `NTFY_TOKEN` | No | ntfy token with read access to the alerts topic. When set, every alert on it is DMed to the owner of the Discord app. |
| `NTFY_URL` | No | ntfy address. Defaults to `http://127.0.0.1:2586`. |
| `NTFY_TOPIC` | No | ntfy topic to forward. Defaults to `homelab`. |
| `STATE_DIRECTORY` | No | Where reminders, time zones and coins are saved (`reminders.json`, `timezones.json`, `casino.json`). Set by systemd's `StateDirectory=`; defaults to the working directory. |

The bot opens outbound Discord/web requests, outbound UDP for voice and
`/server-stats`, and talks to Navidrome; it does not listen on a network port.

### Music

`/music play` joins your voice channel and plays the song or album you pick
from the autocomplete list (or the best match for free text), posting a "now
playing" card with the cover for each song. Navidrome transcodes to Opus, so
the bot only forwards audio. It leaves after 5 minutes with nothing queued.
The bot needs Connect and Speak in the voice channel, and Send Messages,
Embed Links and Attach Files in the text channel.

The app must be added to the server as a bot, not just with
`applications.commands` (that gives working slash commands but no bot member,
so it can't join voice). Invite link with the `bot` scope and those
permissions, using the Application ID from the Developer Portal:

```text
https://discord.com/oauth2/authorize?client_id=APPLICATION_ID&scope=bot+applications.commands&permissions=36752384
```

## Deployment (NixOS home lab)

The bot runs as the `discord-bot` systemd service, defined in
`discord-bot.nix` in the [homelab](https://github.com/winkyfaceak/homelab)
repo. That file has the update and secrets commands. Runtime needs are
ImageMagick (with librsvg), the DejaVu fonts and Discord's
[libdave](https://github.com/discord/libdave), all provided by the service.

## Local Development

Requirements:

- Go 1.26 or newer.
- [libdave](https://github.com/discord/libdave) v1.1.0 (Discord's voice
  encryption library) findable by `pkg-config` as `dave`. godave's
  [install script](https://github.com/disgoorg/godave#libdave-installation)
  sets this up. The packages under `internal/` (and their tests) build
  without it; only the final binary links it.
- ImageMagick with the `magick` executable and SVG support to use interactive
  Deadlock and live server scoreboard cards. Card text uses the DejaVu Sans font.
- Outbound UDP access to the public Source query endpoints used with
  `/server-stats`.

Run the bot locally:

```fish
read -gxs -P 'Discord token: ' DISCORD_TOKEN
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
|-- internal/alerts            # ntfy alerts forwarded to the owner's DMs
|-- internal/bot               # Discord connection and interaction routing
|-- internal/commands          # Slash commands and component handlers
|-- internal/config            # Environment-based configuration
|-- internal/deadlock          # Deadlock API client, summaries, and image cards
|-- internal/discordutil       # Discord response helpers
|-- internal/navidrome         # Navidrome (Subsonic) client and Opus stream reader
|-- internal/serverstats       # Valve A2S querying and live scoreboard cards
`-- internal/weather           # wttr.in integration
```

## External Services

- Discord Gateway and REST API for command registration and interactions.
- [wttr.in](https://wttr.in) for `/weather` results.
- [Deadlock API](https://deadlock-api.com/) for player, match, rank, build, and
  game-status data.
- Public Source-compatible game servers queried via Valve A2S UDP packets for
  `/server-stats` sessions.
- A [Navidrome](https://www.navidrome.org/) server for `/music`.
