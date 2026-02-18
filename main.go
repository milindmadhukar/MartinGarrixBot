package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot/commands"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot/handlers"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot/listeners"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

var Version string
var Commit string

// TODO Member welcome message
// Martin Garrix radio automation
// Info and server info commands

// TODO: Error handling for the bot?
func main() {
	Version = os.Getenv("VERSION")
	Commit = os.Getenv("COMMIT")
	if Version == "" {
		Version = "dev"
	}
	if Commit == "" {
		Commit = "unknown"
	}

	shouldSyncCommands := flag.Bool("sync-commands", false, "Whether to sync commands to discord")
	path := flag.String("config", "config.toml", "path to config")
	shouldClearCommands := flag.Bool("clear-commands", false, "Whether to clear commands from discord")
	flag.Parse()

	cfg, err := mgbot.LoadConfig(*path)
	if err != nil {
		slog.Error("Failed to read config", slog.Any("err", err))
		os.Exit(-1)
	}

	mgbot.SetupLogger(cfg.Log)
	slog.Info("Starting Martin Garrix Bot..", slog.String("version", Version), slog.String("commit", Commit))
	slog.Info("Syncing commands", slog.Bool("sync", *shouldSyncCommands))

	b := mgbot.New(*cfg, Version, Commit)

	// TODO: Disable app commands in DMs
	h := commands.SetupHandlers(b)

	if err = b.SetupDB(); err != nil {
		slog.Error("Failed to setup db", slog.Any("err", err))
		os.Exit(-1)
	}

	service, err := youtube.NewService(context.Background(), option.WithAPIKey(b.Cfg.Bot.YoutubeAPIKey), option.WithCredentialsFile(b.Cfg.Bot.GoogleServiceFile))
	if err != nil {
		slog.Error("Failed to create youtube service", slog.Any("err", err))
		os.Exit(-1)
	}
	b.YoutubeService = service

	if err = b.SetupBot(h,
		bot.NewListenerFunc(b.OnReady),
		listeners.VoiceStateUpdateListener(b),
		listeners.VoiceServerUpdateListener(b),
		listeners.MessageCreateListener(b),
		listeners.GuildMemberJoinListener(b),
		listeners.GuildMemberLeaveListener(b),
		listeners.MessageDeleteListener(b),
		listeners.MessageUpdateListener(b),
	); err != nil {
		slog.Error("Failed to setup bot", slog.Any("err", err))
		os.Exit(-1)
	}

	b.SetupColly()

	// Setup Lavalink (non-blocking, warnings only)
	if err = b.SetupLavalink(context.Background()); err != nil {
		slog.Warn("Failed to setup Lavalink - radio features will be disabled until connection is established", slog.Any("err", err))
		slog.Warn("You can use '/radio start' command to retry connection later")
	} else {
		// Register Lavalink event listeners only if connection succeeded
		b.RegisterLavalinkListeners(
			listeners.LavalinkTrackStartListener(b),
			listeners.LavalinkTrackEndListener(b),
			listeners.LavalinkTrackExceptionListener(b),
			listeners.LavalinkTrackStuckListener(b),
			listeners.LavalinkWebSocketClosedListener(b),
		)
	}

	// TODO: Seems out of place, place somwehere more appropriate
	go func() {
		for {
			if b.IsReady {
				go handlers.GetRedditPosts(b, time.NewTicker(3*time.Minute))
				go handlers.GetYoutubeVideos(b, time.NewTicker(3*time.Minute))
				go handlers.GetAllStmpdReleases(b, time.NewTicker(15*time.Minute))
				go handlers.GetAllTourShows(b, time.NewTicker(10*time.Minute))

				// Auto-start radio in all configured guilds (only if Lavalink is connected)
				go func() {
					time.Sleep(5 * time.Second) // Wait for everything to be ready

					// Only auto-start if Lavalink is connected
					if b.RadioManager == nil || !b.RadioManager.IsLavalinkConnected() {
						slog.Info("Lavalink not connected - skipping auto-start of radio")
						return
					}

					radioConfigs, err := b.Queries.GetRadioVoiceChannels(context.Background())
					if err != nil {
						slog.Error("Failed to get radio configurations", slog.Any("err", err))
						return
					}

					for _, config := range radioConfigs {
						if config.RadioVoiceChannel.Valid {
							guildID := snowflake.ID(config.GuildID)
							slog.Info("Auto-starting radio", slog.String("guild_id", guildID.String()))
							if err := b.StartRadioInGuild(context.Background(), guildID); err != nil {
								slog.Error("Failed to start radio", slog.Any("err", err), slog.String("guild_id", guildID.String()))
							}
						}
					}
				}()

				return
			}
			time.Sleep(1 * time.Second)
		}
	}()

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		b.Client.Close(ctx)
	}()

	if *shouldSyncCommands {
		slog.Info("Syncing commands globally")
		if err = handler.SyncCommands(b.Client, commands.Commands, nil); err != nil {
			slog.Error("Failed to sync commands", slog.Any("err", err))
		}
	}

	if *shouldClearCommands {
		slog.Info("Clearing all commands")
		if err = handler.SyncCommands(b.Client, nil, nil); err != nil {
			slog.Error("Failed to clear commands", slog.Any("err", err))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err = b.Client.OpenGateway(ctx); err != nil {
		slog.Error("Failed to open gateway", slog.Any("err", err))
		os.Exit(-1)
	}

	slog.Info("Bot is running. Press CTRL-C to exit.")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM)
	<-s
	slog.Info("Shutting down bot...")

	// Graceful shutdown: disconnect from all radio channels and clear status
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if b.RadioManager != nil {
		slog.Info("Disconnecting from all radio channels...")
		b.DisconnectAllRadioChannels(shutdownCtx)
	}
}
