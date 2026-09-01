package listeners

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
)

func TestClassifyAuditEntry(t *testing.T) {
	cases := []struct {
		name     string
		action   discord.AuditLogEvent
		wantType string
		wantOK   bool
	}{
		{"kick", discord.AuditLogEventMemberKick, "kick", true},
		{"ban", discord.AuditLogEventMemberBanAdd, "ban", true},
		{"unban", discord.AuditLogEventMemberBanRemove, "unban", true},
		// The audit log carries everything that happens in a server. Only
		// moderation belongs in the modlogs table.
		{"channel created", discord.AuditLogEventChannelCreate, "", false},
		{"role updated", discord.AuditLogEventRoleUpdate, "", false},
		{"message deleted", discord.AuditLogEventMessageDelete, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logType, _, ok := classifyAuditEntry(discord.AuditLogEntry{ActionType: tc.action})
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if logType != tc.wantType {
				t.Errorf("logType = %q, want %q", logType, tc.wantType)
			}
		})
	}
}

// Timeouts arrive as MEMBER_UPDATE with a communication_disabled_until change
// rather than as an action of their own, so they have to be recognised by key.
// They matter because a native timeout is exactly what /moderation mute applies.
func TestClassifyTimeout(t *testing.T) {
	future := time.Now().Add(2 * time.Hour).UTC()

	entry := discord.AuditLogEntry{
		ActionType: discord.AuditLogEventMemberUpdate,
		Changes: []discord.AuditLogChange{{
			Key:      discord.AuditLogChangeKeyCommunicationDisabledUntil,
			NewValue: mustJSON(t, future.Format(time.RFC3339)),
		}},
	}

	logType, expires, ok := classifyAuditEntry(entry)
	if !ok {
		t.Fatal("a timeout should be recorded")
	}
	if logType != "mute" {
		t.Errorf("logType = %q, want mute", logType)
	}
	if !expires.Valid {
		t.Fatal("a timeout should carry its expiry")
	}
	if delta := expires.Time.Sub(future).Abs(); delta > time.Second {
		t.Errorf("expiry = %v, want %v", expires.Time, future)
	}
}

func TestClassifyTimeoutLifted(t *testing.T) {
	for _, raw := range []string{"null", `""`} {
		entry := discord.AuditLogEntry{
			ActionType: discord.AuditLogEventMemberUpdate,
			Changes: []discord.AuditLogChange{{
				Key:      discord.AuditLogChangeKeyCommunicationDisabledUntil,
				NewValue: json.RawMessage(raw),
			}},
		}

		logType, expires, ok := classifyAuditEntry(entry)
		if !ok || logType != "unmute" {
			t.Errorf("new value %s: got (%q, %v), want unmute", raw, logType, ok)
		}
		if expires.Valid {
			t.Error("an unmute has no expiry")
		}
	}
}

// TestClassifyTimeoutExpiryIsNotAnAction: Discord emits this same change as a
// timeout naturally runs out, with a value already in the past. Recording that
// would invent a moderation action nobody took.
func TestClassifyTimeoutExpiryIsNotAnAction(t *testing.T) {
	past := time.Now().Add(-time.Hour).UTC()

	entry := discord.AuditLogEntry{
		ActionType: discord.AuditLogEventMemberUpdate,
		Changes: []discord.AuditLogChange{{
			Key:      discord.AuditLogChangeKeyCommunicationDisabledUntil,
			NewValue: mustJSON(t, past.Format(time.RFC3339)),
		}},
	}

	if _, _, ok := classifyAuditEntry(entry); ok {
		t.Error("a timeout expiring in the past was recorded as a new action")
	}
}

// A nickname or role edit is a MEMBER_UPDATE too, and is not moderation.
func TestClassifyMemberUpdateWithoutTimeoutIsIgnored(t *testing.T) {
	entry := discord.AuditLogEntry{
		ActionType: discord.AuditLogEventMemberUpdate,
		Changes: []discord.AuditLogChange{{
			Key:      discord.AuditLogChangeKeyNick,
			NewValue: mustJSON(t, "new nickname"),
		}},
	}

	if _, _, ok := classifyAuditEntry(entry); ok {
		t.Error("a nickname change was recorded as moderation")
	}
}

func TestParseAuditTime(t *testing.T) {
	t.Run("null is the zero time", func(t *testing.T) {
		got, err := parseAuditTime(json.RawMessage("null"))
		if err != nil {
			t.Fatalf("parseAuditTime: %v", err)
		}
		if !got.IsZero() {
			t.Errorf("got %v, want the zero time", got)
		}
	})

	t.Run("absent is the zero time", func(t *testing.T) {
		got, err := parseAuditTime(nil)
		if err != nil || !got.IsZero() {
			t.Errorf("got (%v, %v)", got, err)
		}
	})

	t.Run("garbage is an error", func(t *testing.T) {
		if _, err := parseAuditTime(json.RawMessage(`"not a time"`)); err == nil {
			t.Error("expected an error")
		}
	})
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling fixture: %v", err)
	}
	return raw
}
