package mgbot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/paginator"
	"github.com/gocolly/colly/v2"
	"github.com/golang-migrate/migrate/v4"
	migratePgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxStdlib "github.com/jackc/pgx/v5/stdlib"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
	"google.golang.org/api/youtube/v3"
	"gopkg.in/natefinch/lumberjack.v2"
)

func New(cfg Config, version string, commit string) *MartinGarrixBot {
	return &MartinGarrixBot{
		Cfg:       cfg,
		Paginator: paginator.New(),
		Version:   version,
		Commit:    commit,
	}
}

type MartinGarrixBot struct {
	Cfg       Config
	Client    bot.Client
	Paginator *paginator.Manager
	Version   string
	Commit    string
	IsReady   bool

	DB             *pgxpool.Pool
	Queries        *db.Queries
	YoutubeService *youtube.Service

	Collector *colly.Collector

	RedditToken  utils.RedditToken
	RadioManager *utils.RadioManager
}

func (b *MartinGarrixBot) SetupBot(listeners ...bot.EventListener) error {
	client, err := disgo.New(b.Cfg.Bot.Token,
		bot.WithGatewayConfigOpts(gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildMessages, gateway.IntentMessageContent, gateway.IntentGuildMembers, gateway.IntentGuildVoiceStates)),
		bot.WithCacheConfigOpts(cache.WithCaches(cache.FlagGuilds, cache.FlagMessages, cache.FlagVoiceStates, cache.FlagMembers)),
		bot.WithEventListeners(b.Paginator),
		bot.WithEventListeners(listeners...),
	)
	if err != nil {
		return err
	}

	b.Client = client

	return nil
}

// TODO: Make foreign key constraints on tables
func (b *MartinGarrixBot) SetupDB() error {
	tries := 5
	DBConn, err := pgxpool.New(context.Background(), b.Cfg.DB.URI())
	if err != nil {
		return err
	}

	for tries > 0 {
		slog.Info("Attempting to make a connection to the garrixbot database...")
		err = DBConn.Ping(context.Background())
		if err != nil {
			tries -= 1
			slog.Info(err.Error() + "\nCould not connect. Retrying...")
			time.Sleep(5 * time.Second)
			continue
		}
		b.Queries = db.New(DBConn)
		b.DB = DBConn
		slog.Info("Connection to the garrixbot database established.")

		driver, err := migratePgx.WithInstance(
			pgxStdlib.OpenDBFromPool(DBConn),
			&migratePgx.Config{},
		)

		if err != nil {
			return err
		}

		m, err := migrate.NewWithDatabaseInstance(
			"file://db/migrations",
			"postgres", driver)

		if err != nil {
			return err
		}

		if err = m.Up(); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				slog.Info("Database is already up to date.")
				return nil
			}

			return err
		}

		slog.Info("Database migrated to latest migration.")
		return nil
	}
	return errors.New("Could not make a connection to the database.")
}

func (b *MartinGarrixBot) SetupColly() {
	b.Collector = colly.NewCollector(
		colly.AllowedDomains("stmpdrcrds.com", "martingarrix.com"),
		colly.Async(true),
		colly.AllowURLRevisit(),
	)

	b.Collector.Limit(&colly.LimitRule{
		Parallelism: 2,
		RandomDelay: 2 * time.Second,
	})
}

func (b *MartinGarrixBot) OnReady(e *events.Ready) {
	slog.Info("Martin Garrix Bot ready")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	slog.Info("Bot Name: " + e.User.Username)
	slog.Info("Bot ID: " + e.User.ID.String())
	slog.Info(fmt.Sprintf("Total Guilds: %d", len(e.Guilds)))

	// TODO: Update presence
	if err := b.Client.SetPresence(ctx, gateway.WithListeningActivity("you"), gateway.WithOnlineStatus(discord.OnlineStatusOnline)); err != nil {
		slog.Error("Failed to set presence", slog.Any("err", err))
	}

	b.IsReady = true
}

func SetupLogger(cfg LogConfig) {
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

// SetupLavalink initializes the Lavalink client and connects to the node
func (b *MartinGarrixBot) SetupLavalink(ctx context.Context) error {
	b.RadioManager = utils.NewRadioManager(b.Client.ApplicationID())

	// Set up disconnect callback
	b.RadioManager.OnLavalinkDisconnect = func() {
		slog.Error("Lavalink permanently disconnected - disconnecting from all radio channels")
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b.DisconnectAllRadioChannels(disconnectCtx)
	}

	// Connect to Lavalink with retry logic
	if err := b.RadioManager.ConnectToLavalink(ctx, b.Cfg.Lavalink.URL, b.Cfg.Lavalink.Password); err != nil {
		return err
	}

	// Start monitoring Lavalink connection (max 10 reconnect attempts = ~50 seconds)
	go b.RadioManager.MonitorLavalinkConnection(10)

	return nil
}

// RegisterLavalinkListeners registers Lavalink event listeners
// This should be called from main.go after SetupLavalink to avoid import cycles
func (b *MartinGarrixBot) RegisterLavalinkListeners(eventListeners ...disgolink.EventListener) {
	b.RadioManager.Client.AddListeners(eventListeners...)
}
