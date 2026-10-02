package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"discord_gobot/internal/alerts"
	"discord_gobot/internal/commands"
	"discord_gobot/internal/config"

	"github.com/disgoorg/disgo"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave/golibdave"
)

// Bot owns the command list and routes Discord interactions to them
type Bot struct {
	// cfg stores the token and optional guild ID
	cfg config.Config

	// commands stores every command implementation.
	commands []commands.Command

	// commandByName lets us quickly find the correct handler when a user runs
	// a slash command
	commandByName map[string]commands.Command

	// componentByPrefix routes Discord button/select-menu custom IDs.
	componentByPrefix map[string]commands.ComponentCommand
}

// New creates a Bot but does not connect to Discord yet
func New(cfg config.Config, cmds []commands.Command) (*Bot, error) {
	b := &Bot{
		cfg:               cfg,
		commands:          cmds,
		commandByName:     make(map[string]commands.Command),
		componentByPrefix: make(map[string]commands.ComponentCommand),
	}

	// Build a lookup table:
	//
	//   "ping"    -> Ping{}
	//   "weather" -> Weather{}
	//
	// This makes interaction handling simple later
	for _, cmd := range cmds {
		name := cmd.Definition().CommandName()
		if name == "" {
			return nil, fmt.Errorf("command has empty name")
		}
		if _, exists := b.commandByName[name]; exists {
			return nil, fmt.Errorf("duplicate command name: %s", name)
		}
		b.commandByName[name] = cmd

		if componentCommand, ok := cmd.(commands.ComponentCommand); ok {
			prefix := strings.TrimSpace(componentCommand.ComponentPrefix())
			if prefix == "" {
				return nil, fmt.Errorf("component command /%s has empty component prefix", name)
			}
			if _, exists := b.componentByPrefix[prefix]; exists {
				return nil, fmt.Errorf("duplicate component prefix: %s", prefix)
			}
			b.componentByPrefix[prefix] = componentCommand
		}
	}

	return b, nil
}

// Run connects to Discord and keeps the bot alive until the process is interrupted
func (b *Bot) Run() error {
	opts := []disgobot.ConfigOpt{
		// Guilds for the server list, voice states to find the caller's voice channel
		disgobot.WithGatewayConfigOpts(gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildVoiceStates)),
		disgobot.WithCacheConfigOpts(cache.WithCaches(cache.FlagGuilds, cache.FlagVoiceStates)),
		// Handlers do slow API work, so don't block the gateway on them
		disgobot.WithEventManagerConfigOpts(disgobot.WithAsyncEventsEnabled()),
		// Text the bot echoes back (e.g. a /weather location of @everyone) never pings anyone
		disgobot.WithRestConfigOpts(rest.WithDefaultAllowedMentions(discord.AllowedMentions{Parse: []discord.AllowedMentionType{}})),
		// Discord requires DAVE end-to-end encryption for voice
		disgobot.WithVoiceManagerConfigOpts(voice.WithDaveSessionCreateFunc(golibdave.NewSession)),
		disgobot.WithEventListenerFunc(onReady),
		disgobot.WithEventListenerFunc(b.removeStaleGuildCommands),
		disgobot.WithEventListenerFunc(b.onCommand),
		disgobot.WithEventListenerFunc(b.onComponent),
		disgobot.WithEventListenerFunc(b.onAutocomplete),
	}
	for _, cmd := range b.commands {
		if listener, ok := cmd.(disgobot.EventListener); ok {
			opts = append(opts, disgobot.WithEventListeners(listener))
		}
	}

	client, err := disgo.New(b.cfg.Token, opts...)
	if err != nil {
		return fmt.Errorf("create Discord client: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client.Close(ctx)
	}()

	if err := b.registerCommands(client); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.OpenGateway(ctx); err != nil {
		return fmt.Errorf("open Discord gateway: %w", err)
	}

	// Wait for CTRL+C or a termination signal (systemctl stop sends SIGTERM)
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if b.cfg.NtfyToken != "" {
		go alerts.Forward(stopCtx, client, b.cfg.NtfyURL, b.cfg.NtfyTopic, b.cfg.NtfyToken)
		log.Printf("Forwarding ntfy topic %q to the app owner's DMs.", b.cfg.NtfyTopic)
	}

	log.Println("Bot is running. Press CTRL+C to stop.")
	<-stopCtx.Done()

	for _, cmd := range b.commands {
		if shutdownCommand, ok := cmd.(commands.ShutdownCommand); ok {
			shutdownCommand.Shutdown()
		}
	}

	log.Println("Bot stopped.")
	return nil
}

// registerCommands replaces the app's command list with ours in one call.
//
// Bulk overwrite is idempotent, so restarts don't burn Discord's daily
// command-create limit, and commands removed from the code disappear.
//
// If GUILD_ID is set, commands are registered to that one server
// If GUILD_ID is empty, commands are registered globally
func (b *Bot) registerCommands(client *disgobot.Client) error {
	defs := make([]discord.ApplicationCommandCreate, 0, len(b.commands))
	for _, cmd := range b.commands {
		defs = append(defs, cmd.Definition())
	}

	var created []discord.ApplicationCommand
	var err error
	if b.cfg.GuildID == 0 {
		created, err = client.Rest.SetGlobalCommands(client.ApplicationID, defs)
	} else {
		created, err = client.Rest.SetGuildCommands(client.ApplicationID, b.cfg.GuildID, defs)
	}
	if err != nil {
		return fmt.Errorf("register slash commands: %w", err)
	}

	for _, cmd := range created {
		log.Printf("Registered command: /%s", cmd.Name())
	}
	if b.cfg.GuildID == 0 {
		log.Println("Slash commands registered globally.")
	} else {
		log.Printf("Slash commands registered for guild/server ID: %s", b.cfg.GuildID)
	}

	return nil
}

// removeStaleGuildCommands deletes server-only commands left over from runs
// with GUILD_ID set. Global registration never touches them, so removed
// commands would otherwise linger in that server.
func (b *Bot) removeStaleGuildCommands(e *events.GuildReady) {
	if b.cfg.GuildID != 0 {
		return
	}
	client := e.Client()
	stale, err := client.Rest.GetGuildCommands(client.ApplicationID, e.GuildID, false)
	if err != nil || len(stale) == 0 {
		if err != nil {
			log.Printf("check server-only commands in %s: %v", e.GuildID, err)
		}
		return
	}
	if _, err := client.Rest.SetGuildCommands(client.ApplicationID, e.GuildID, []discord.ApplicationCommandCreate{}); err != nil {
		log.Printf("remove server-only commands in %s: %v", e.GuildID, err)
		return
	}
	names := make([]string, 0, len(stale))
	for _, cmd := range stale {
		names = append(names, "/"+cmd.Name())
	}
	log.Printf("Removed %d old server-only commands from %s: %s", len(stale), e.Guild.Name, strings.Join(names, " "))
}

// onReady runs when Discord confirms the bot is connected
func onReady(e *events.Ready) {
	log.Printf("Logged in as %s (application ID %s)", e.User.Username, e.User.ID)
}

func (b *Bot) onCommand(e *events.ApplicationCommandInteractionCreate) {
	name := e.Data.CommandName()

	cmd, ok := b.commandByName[name]
	if !ok {
		log.Printf("No handler found for command: /%s", name)
		return
	}

	cmd.Handle(e)
}

func (b *Bot) onComponent(e *events.ComponentInteractionCreate) {
	prefix, _, _ := strings.Cut(e.Data.CustomID(), ":")

	handler, ok := b.componentByPrefix[prefix]
	if !ok {
		log.Printf("No component handler found for custom ID prefix: %s", prefix)
		return
	}

	handler.HandleComponent(e)
}

func (b *Bot) onAutocomplete(e *events.AutocompleteInteractionCreate) {
	if cmd, ok := b.commandByName[e.Data.CommandName].(commands.AutocompleteCommand); ok {
		cmd.HandleAutocomplete(e)
	}
}
