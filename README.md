# Discord Go Bot

A Discord slash-command bot written in Go. It connects outbound to Discord, so it
does not expose a listening port.

## Docker Deployment

Build the image:

```sh
docker build -t discord-gobot .
```

Run it with the required Discord token and optional command registration
settings:

```sh
docker run -d \
  --name discord-gobot \
  --restart unless-stopped \
  -e DISCORD_TOKEN="your_raw_bot_token_here" \
  -e GUILD_ID="your_test_server_id_here" \
  -e APP_ID="your_application_id_here" \
  discord-gobot
```

`DISCORD_TOKEN` is required. `GUILD_ID` restricts slash command registration to
one Discord server, which is useful while developing. `APP_ID` is optional
because the bot can discover its application ID once it connects.

## Docker Compose

Export configuration into the environment, then start the service:

```sh
export DISCORD_TOKEN="your_raw_bot_token_here"
export GUILD_ID="your_test_server_id_here"
export APP_ID="your_application_id_here"
docker compose up -d --build
```

View logs or stop the service with:

```sh
docker compose logs -f bot
docker compose down
```

The runtime image includes `curl` for weather lookups and ImageMagick SVG
support plus fonts for generated Deadlock statistics cards. Configuration
remains in environment variables; do not bake bot tokens into the image.
