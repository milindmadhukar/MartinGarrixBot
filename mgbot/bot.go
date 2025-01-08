package mgbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/paginator"
	"github.com/golang-migrate/migrate/v4"
	migratePgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	pgxStdlib "github.com/jackc/pgx/v5/stdlib"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
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

	// TODO: Add the db here
	DB      *pgx.Conn
	Queries *db.Queries
}

func (b *MartinGarrixBot) SetupBot(listeners ...bot.EventListener) error {
	client, err := disgo.New(b.Cfg.Bot.Token,
		bot.WithGatewayConfigOpts(gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildMessages, gateway.IntentMessageContent)),
		bot.WithCacheConfigOpts(cache.WithCaches(cache.FlagGuilds)),
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
	DBConn, err := pgx.Connect(context.Background(), b.Cfg.DB.URI())
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
			pgxStdlib.OpenDB(*DBConn.Config()),
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

		m.Up()

		slog.Info("Database migrated to latest migration.")

		return nil
	}
	return errors.New("Could not make a connection to the database.")
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
}