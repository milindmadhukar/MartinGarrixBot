package listeners

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
)

// GuildAuditLogListener records moderation performed through Discord's own UI.
//
// Until this existed, `modlogs` was only ever written by the bot's /moderation
// commands, so a server whose staff use the native kick and ban buttons showed
// zero moderation actions on the dashboard. Discord's audit log carries exactly
// the shape the table already stores -- who was actioned, by whom, why -- so
// these rows are indistinguishable from command-created ones apart from
// audit_log_id being set.
//
// Requires gateway.IntentGuildModeration AND the View Audit Log permission in
// the guild. Without the permission Discord simply never sends the event, which
// is why the dashboard checks for it and says so rather than showing an empty
// table.
func GuildAuditLogListener(b *stmpdbot.STMPDBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildAuditLogEntryCreate) {
		entry := e.AuditLogEntry

		// The bot's own actions are already recorded by the command that
		// performed them, and that row names the human moderator who ran it.
		// Recording the audit entry too would double-count every action and
		// attribute it to the bot.
		if entry.UserID == e.Client().ApplicationID {
			return
		}

		logType, expiresAt, ok := classifyAuditEntry(entry)
		if !ok {
			return
		}

		if entry.TargetID == nil {
			// Every action we care about targets a user; an entry without one
			// is not something this table can represent.
			return
		}

		reason := pgtype.Text{}
		if entry.Reason != nil && *entry.Reason != "" {
			reason = pgtype.Text{String: *entry.Reason, Valid: true}
		}

		err := b.Queries.CreateModlogFromAudit(context.Background(), db.CreateModlogFromAuditParams{
			UserID:      int64(*entry.TargetID),
			ModeratorID: int64(entry.UserID),
			GuildID:     int64(e.GuildID),
			LogType:     logType,
			Reason:      reason,
			ExpiresAt:   expiresAt,
			Active:      pgtype.Bool{Bool: true, Valid: true},
			AuditLogID:  pgtype.Int8{Int64: int64(entry.ID), Valid: true},
		})
		if err != nil {
			slog.Error("Failed to record audit log moderation",
				slog.Any("err", err),
				slog.String("guild_id", e.GuildID.String()),
				slog.String("log_type", logType))
			return
		}

		slog.Info("Recorded moderation from Discord",
			slog.String("guild_id", e.GuildID.String()),
			slog.String("log_type", logType),
			slog.String("target_id", entry.TargetID.String()),
			slog.String("moderator_id", entry.UserID.String()))

		stmpdbot.SendModlogEmbed(b, e.GuildID, *entry.TargetID, entry.UserID,
			logType, reason.String, expiresAtPtr(expiresAt))
	})
}

// classifyAuditEntry maps an audit entry onto a modlogs log_type.
//
// The timeout cases go through MEMBER_UPDATE rather than a dedicated action,
// so they have to be recognised by the change key. They are worth capturing
// because a native timeout is the same thing /moderation mute applies, so both
// routes end up as the same log_type.
func classifyAuditEntry(entry discord.AuditLogEntry) (logType string, expiresAt pgtype.Timestamp, ok bool) {
	switch entry.ActionType {
	case discord.AuditLogEventMemberKick:
		return "kick", pgtype.Timestamp{}, true
	case discord.AuditLogEventMemberBanAdd:
		return "ban", pgtype.Timestamp{}, true
	case discord.AuditLogEventMemberBanRemove:
		return "unban", pgtype.Timestamp{}, true
	case discord.AuditLogEventMemberUpdate:
		return classifyMemberUpdate(entry)
	default:
		// Channel edits, role changes, emoji uploads and everything else in the
		// audit log are not moderation and must not land in this table.
		return "", pgtype.Timestamp{}, false
	}
}

func classifyMemberUpdate(entry discord.AuditLogEntry) (string, pgtype.Timestamp, bool) {
	for _, change := range entry.Changes {
		if change.Key != discord.AuditLogChangeKeyCommunicationDisabledUntil {
			continue
		}

		// A null or absent new value means the timeout was lifted.
		until, err := parseAuditTime(change.NewValue)
		if err != nil || until.IsZero() {
			return "unmute", pgtype.Timestamp{}, true
		}

		// Discord also emits this change as a timeout naturally expires, with a
		// new value already in the past. That is not a moderator action and
		// should not appear as one.
		if !until.After(time.Now()) {
			return "", pgtype.Timestamp{}, false
		}

		return "mute", pgtype.Timestamp{Time: until.UTC(), Valid: true}, true
	}
	// A nickname or role change: real, but not moderation.
	return "", pgtype.Timestamp{}, false
}

// parseAuditTime reads the ISO8601 timestamp Discord sends for
// communication_disabled_until. A JSON null unmarshals to the zero time, which
// the caller reads as "timeout lifted".
func parseAuditTime(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return time.Time{}, err
	}
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

func expiresAtPtr(ts pgtype.Timestamp) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}
