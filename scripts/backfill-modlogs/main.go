// Command backfill-modlogs imports historical moderation from Discord's audit
// log into the modlogs table.
//
// The audit-log listener only sees actions taken after it starts, so a server
// that has been moderating through Discord's own UI for months still shows an
// empty moderation log on the dashboard the day the listener ships. Discord
// retains roughly 45 days of audit history; this walks it once and inserts what
// it finds.
//
// Safe to re-run: every row is keyed on the audit entry's ID and inserted with
// ON CONFLICT DO NOTHING, so a second pass over the same window writes nothing.
//
//	go run ./scripts/backfill-modlogs -config=config.toml -guild=<id> -dry-run
//	go run ./scripts/backfill-modlogs -config=config.toml -guild=<id>
package main

import (
	"flag"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
)

// pageSize is Discord's maximum for this endpoint.
const pageSize = 100

var guildFlag = flag.String("guild", "", "guild ID to backfill (required)")

func main() {
	env, ctx, release := script.Setup("backfill-modlogs")
	defer release()

	if *guildFlag == "" {
		script.Fatal("missing -guild", nil)
	}
	guildID, err := snowflake.Parse(*guildFlag)
	if err != nil {
		script.Fatal("invalid -guild", err)
	}

	// A bare REST client: this script must never open a gateway connection or
	// touch the notifier, so it cannot announce anything to Discord.
	client := rest.New(rest.NewClient(env.Config.Bot.Token))

	// The bot's own actions already have rows written by the command that
	// performed them, naming the human moderator rather than the bot.
	self, err := client.GetBotApplicationInfo()
	if err != nil {
		script.Fatal("failed to identify the bot application", err)
	}

	var inserted, skipped, seen int

	for _, action := range []discord.AuditLogEvent{
		discord.AuditLogEventMemberKick,
		discord.AuditLogEventMemberBanAdd,
		discord.AuditLogEventMemberBanRemove,
	} {
		// Walk backwards through the history a page at a time. `before` is the
		// oldest entry seen so far, so each request continues where the last
		// one stopped.
		var before snowflake.ID

		for {
			log, err := client.GetAuditLog(guildID, 0, action, before, 0, pageSize)
			if err != nil {
				script.Fatal("failed to read the audit log (does the bot have View Audit Log?)", err)
			}
			if len(log.AuditLogEntries) == 0 {
				break
			}

			for _, entry := range log.AuditLogEntries {
				seen++
				before = entry.ID

				if entry.TargetID == nil || entry.UserID == self.ID {
					skipped++
					continue
				}

				reason := pgtype.Text{}
				if entry.Reason != nil && *entry.Reason != "" {
					reason = pgtype.Text{String: *entry.Reason, Valid: true}
				}

				err := env.Queries.CreateModlogFromAudit(ctx, db.CreateModlogFromAuditParams{
					UserID:      int64(*entry.TargetID),
					ModeratorID: int64(entry.UserID),
					GuildID:     int64(guildID),
					LogType:     logTypeFor(action),
					Reason:      reason,
					ExpiresAt:   pgtype.Timestamp{},
					Active:      pgtype.Bool{Bool: true, Valid: true},
					AuditLogID:  pgtype.Int8{Int64: int64(entry.ID), Valid: true},
				})
				if err != nil {
					script.Fatal("failed to insert a modlog row", err)
				}
				inserted++
			}

			if len(log.AuditLogEntries) < pageSize {
				break
			}
			// Discord rate-limits this endpoint; the REST client honours
			// Retry-After, but pacing keeps a large backfill from spending its
			// time in backoff.
			time.Sleep(time.Second)
		}
	}

	slog.Info("Backfill complete",
		slog.String("guild_id", guildID.String()),
		slog.Int("entries_seen", seen),
		slog.Int("inserted", inserted),
		slog.Int("skipped_bot_or_targetless", skipped),
		slog.Bool("dry_run", env.DryRun))
}

func logTypeFor(action discord.AuditLogEvent) string {
	switch action {
	case discord.AuditLogEventMemberKick:
		return "kick"
	case discord.AuditLogEventMemberBanAdd:
		return "ban"
	case discord.AuditLogEventMemberBanRemove:
		return "unban"
	default:
		return "unknown"
	}
}
