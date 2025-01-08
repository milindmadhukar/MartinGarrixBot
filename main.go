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
	"github.com/milindmadhukar/MartinGarrixBot/mgbot/components"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot/handlers"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

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
	h.Command("/version", commands.VersionHandler(b))
	h.Component("/test-button", components.TestComponent)

	if err = b.SetupDB(); err != nil {
		slog.Error("Failed to setup db", slog.Any("err", err))
		os.Exit(-1)
	}

	if err = b.SetupBot(h, bot.NewListenerFunc(b.OnReady), handlers.MessageHandler(b)); err != nil {
		slog.Error("Failed to setup bot", slog.Any("err", err))
		os.Exit(-1)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		b.Client.Close(ctx)
	}()

	if *shouldSyncCommands {
		slog.Info("Syncing commands", slog.Any("guild_ids", cfg.Bot.DevGuilds))
		if err = handler.SyncCommands(b.Client, commands.Commands, cfg.Bot.DevGuilds); err != nil {
			slog.Error("Failed to sync commands", slog.Any("err", err))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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