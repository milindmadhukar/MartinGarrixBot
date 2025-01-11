package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot/commands"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot/handlers"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

// TODO: Error handling for the bot?
func main() {
	shouldSyncCommands := flag.Bool("sync-commands", false, "Whether to sync commands to discord")
	path := flag.String("config", "config.toml", "path to config")
	flag.Parse()

	cfg, err := mgbot.LoadConfig(*path)
	if err != nil {
		slog.Error("Failed to read config", slog.Any("err", err))
		os.Exit(-1)
	}

	setupLogger(cfg.Log)
	slog.Info("Starting Martin Garrix Bot..", slog.String("version", Version), slog.String("commit", Commit))
	slog.Info("Syncing commands", slog.Bool("sync", *shouldSyncCommands))

	b := mgbot.New(*cfg, Version, Commit)

	h := handler.New()

	h.Command("/ping", commands.PingHandler)
	h.Command("/avatar", commands.AvatarHandler)
	h.Command("/8ball", commands.EightBallHandler)
	h.Command("/lyrics", commands.LyricsHandler(b))
	h.Autocomplete("/lyrics", commands.LyricsAutocompleteHandler(b))
	h.Command("/quiz", commands.QuizHandler(b))
	h.Command("/balance", commands.BalanceHandler(b))
	h.Command("/withdraw", commands.WithdrawHandler(b))
	h.Command("/deposit", commands.DepositHandler(b))
	// h.Command("/whois", commands.WhoisHandler)
	h.Command("/version", commands.VersionHandler(b))

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

	if err = b.SetupBot(h, bot.NewListenerFunc(b.OnReady), handlers.MessageHandler(b)); err != nil {
		slog.Error("Failed to setup bot", slog.Any("err", err))
		os.Exit(-1)
	}

	// TODO: Seems out of place, place somwehere more appropriate
	go handlers.GetRedditPosts(b, time.NewTicker(3*time.Minute))
	go handlers.GetYoutubeVideos(b, time.NewTicker(3*time.Minute))

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		b.Client.Close(ctx)
	}()

	if *shouldSyncCommands {
		slog.Info("Syncing commands", slog.Any("guild_ids", cfg.Bot.DevGuilds))
		if err = handler.SyncCommands(b.Client, commands.Commands, cfg.Bot.DevGuilds); err != nil {
			slog.Error("Failed to sync commands", slog.Any("err", err))
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
}

func setupLogger(cfg mgbot.LogConfig) {
	opts := &slog.HandlerOptions{
		AddSource: cfg.AddSource,
		Level:     cfg.Level,
	}

	fileWriter := &lumberjack.Logger{
		Filename:   cfg.File,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   true,
	}

	multiWriter := io.MultiWriter(os.Stdout, fileWriter)

	var sHandler slog.Handler
	switch cfg.Format {
	case "json":
		sHandler = slog.NewJSONHandler(multiWriter, opts)
	case "text":
		sHandler = slog.NewTextHandler(multiWriter, opts)
	default:
		slog.Error("Unknown log format", slog.String("format", cfg.Format))
		os.Exit(-1)
	}

	slog.SetDefault(slog.New(sHandler))
}