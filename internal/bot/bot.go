package bot

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"discord_gobot/internal/commands"
	"discord_gobot/internal/config"

	"github.com/bwmarrin/discordgo"
)

// Bot owns the Discord session and the registered command list
//
// The session is the main DiscordGo object
// Most Discord API actions happen through *discordgo.Session
type Bot struct {
	// session is the active DiscordGo connection/client
	session *discordgo.Session

	// cfg stores token, guild ID, and app ID
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
//
// This function wires together:
//   - the DiscordGo session
//   - event handlers
//   - slash command lookup
func New(cfg config.Config, cmds []commands.Command) (*Bot, error) {
	// DiscordGo expects the authorization format to include "Bot ".
	// We keep DISCORD_TOKEN as the raw token and add the prefix here.
	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create Discord session: %w", err)
	}

	b := &Bot{
		session:           session,
		cfg:               cfg,
		commands:          cmds,
		commandByName:     make(map[string]commands.Command),
		componentByPrefix: make(map[string]commands.ComponentCommand),
	}

	// Build a lookup table:
	//
	//   "ping"  -> Ping{}
	//   "hello" -> Hello{}
	//   "echo"  -> Echo{}
	//
	// This makes interaction handling simple later
	for _, cmd := range cmds {
		def := cmd.Definition()

		if def == nil {
			return nil, fmt.Errorf("command returned nil definition")
		}

		if def.Name == "" {
			return nil, fmt.Errorf("command has empty name")
		}

		if _, exists := b.commandByName[def.Name]; exists {
			return nil, fmt.Errorf("duplicate command name: %s", def.Name)
		}

		b.commandByName[def.Name] = cmd

		if componentCommand, ok := cmd.(commands.ComponentCommand); ok {
			prefix := strings.TrimSpace(componentCommand.ComponentPrefix())
			if prefix == "" {
				return nil, fmt.Errorf("component command /%s has empty component prefix", def.Name)
			}
			if _, exists := b.componentByPrefix[prefix]; exists {
				return nil, fmt.Errorf("duplicate component prefix: %s", prefix)
			}
			b.componentByPrefix[prefix] = componentCommand
		}
	}

	// Register event handlers
	//
	// DiscordGo calls these functions when matching events arrive
	session.AddHandler(b.onReady)
	session.AddHandler(b.onInteractionCreate)

	// Slash commands do not require MessageContent intent
	//
	// MessageContent is only needed when reading normal chat messages
	// Since this bot uses slash commands, Guilds intent is enough here
	session.Identify.Intents = discordgo.IntentsGuilds

	return b, nil
}

// Run starts the bot and keeps it alive until the process is interrupted
func (b *Bot) Run() error {
	// Open starts the websocket connection to Discord
	//
	// After this succeeds, the bot can receive events
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open Discord session: %w", err)
	}

	// Always close the session when Run exits
	defer b.session.Close()

	// Get the Discord application ID
	//
	// Slash commands are registered against an application ID
	appID, err := b.resolveAppID()
	if err != nil {
		return err
	}

	// Register slash commands with Discord
	if err := b.registerCommands(appID); err != nil {
		return err
	}

	if b.cfg.GuildID == "" {
		log.Println("Slash commands registered globally.")
		log.Println("Global commands can take longer to appear.")
	} else {
		log.Printf("Slash commands registered for guild/server ID: %s", b.cfg.GuildID)
	}

	log.Println("Bot is running. Press CTRL+C to stop.")

	// Wait for CTRL+C or a termination signal
	//
	// Without this, main would exit immediately and the bot would disconnect
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-stop

	for _, cmd := range b.commands {
		if shutdownCommand, ok := cmd.(commands.ShutdownCommand); ok {
			shutdownCommand.Shutdown()
		}
	}

	log.Println("Bot stopped.")
	return nil
}

// resolveAppID returns the application/client ID used to register commands
//
// You can provide APP_ID manually, or let the bot try to discover it
func (b *Bot) resolveAppID() (string, error) {
	// Prefer explicit APP_ID from the environment
	if b.cfg.AppID != "" {
		return b.cfg.AppID, nil
	}

	// After the session opens, DiscordGo usually has the bot user in state
	if b.session.State != nil &&
		b.session.State.User != nil &&
		b.session.State.User.ID != "" {
		return b.session.State.User.ID, nil
	}

	// Fallback: ask Discord who the bot is
	user, err := b.session.User("@me")
	if err != nil {
		return "", fmt.Errorf("get bot user/application ID: %w", err)
	}

	return user.ID, nil
}

// registerCommands sends each command definition to Discord
//
// If GUILD_ID is set, commands are registered to that one server
// If GUILD_ID is empty, commands are registered globally
func (b *Bot) registerCommands(appID string) error {
	log.Println("Registering slash commands...")

	for _, cmd := range b.commands {
		def := cmd.Definition()

		created, err := b.session.ApplicationCommandCreate(
			appID,
			b.cfg.GuildID,
			def,
		)
		if err != nil {
			return fmt.Errorf("register command /%s: %w", def.Name, err)
		}

		log.Printf("Registered command: /%s", created.Name)
	}

	return nil
}

// onReady runs when Discord confirms the bot is connected
func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	if s.State == nil || s.State.User == nil {
		log.Println("Bot connected, but user state is not available")
		return
	}

	log.Printf(
		"Logged in as %s#%s",
		s.State.User.Username,
		s.State.User.Discriminator,
	)
}

// onInteractionCreate runs when a Discord interaction is created.
//
// Slash commands, buttons, select menus, and modals are all interactions.
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleApplicationCommand(s, i)
	case discordgo.InteractionMessageComponent:
		b.handleMessageComponent(s, i)
	}
}

func (b *Bot) handleApplicationCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	name := i.ApplicationCommandData().Name

	cmd, ok := b.commandByName[name]
	if !ok {
		log.Printf("No handler found for command: /%s", name)
		return
	}

	cmd.Handle(s, i)
}

func (b *Bot) handleMessageComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	prefix := customID
	if separator := strings.Index(customID, ":"); separator >= 0 {
		prefix = customID[:separator]
	}

	handler, ok := b.componentByPrefix[prefix]
	if !ok {
		log.Printf("No component handler found for custom ID prefix: %s", prefix)
		return
	}

	handler.HandleComponent(s, i)
}
