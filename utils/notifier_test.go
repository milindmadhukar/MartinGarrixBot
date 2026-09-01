package utils

// In-package: buildHeaderContent is unexported. It reads only Items and
// NotificationType, so the DB and REST fields can stay nil.

import (
	"testing"

	"github.com/disgoorg/snowflake/v2"
)

func TestBuildHeaderContent(t *testing.T) {
	t.Parallel()

	roleID := snowflake.ID(987654321)

	tests := []struct {
		name      string
		notifType NotificationType
		items     int
		roleID    *snowflake.ID
		want      string
	}{
		{
			name:      "one youtube video",
			notifType: NotificationTypeYoutube,
			items:     1,
			want:      "New video posted!",
		},
		{
			name:      "several youtube videos",
			notifType: NotificationTypeYoutube,
			items:     3,
			want:      "3 new videos posted!",
		},
		{
			name:      "one reddit post",
			notifType: NotificationTypeReddit,
			items:     1,
			want:      "New post on r/Martingarrix",
		},
		{
			name:      "several reddit posts",
			notifType: NotificationTypeReddit,
			items:     2,
			want:      "2 new posts on r/Martingarrix",
		},
		{
			name:      "one stmpd release",
			notifType: NotificationTypeSTMPD,
			items:     1,
			want:      "New release on STMPD RCRDS!",
		},
		{
			name:      "several stmpd releases",
			notifType: NotificationTypeSTMPD,
			items:     5,
			want:      "5 new releases on STMPD RCRDS!",
		},
		{
			name:      "one tour date",
			notifType: NotificationTypeTour,
			items:     1,
			want:      "New tour date announced! 🎤",
		},
		{
			name:      "several tour dates",
			notifType: NotificationTypeTour,
			items:     4,
			want:      "4 new tour dates announced! 🎤",
		},
		{
			name:      "an unknown type falls back",
			notifType: NotificationType("something-else"),
			items:     1,
			want:      "New notification",
		},
		{
			name:      "a role is pinged before the message",
			notifType: NotificationTypeYoutube,
			items:     1,
			roleID:    &roleID,
			want:      "<@&987654321>, New video posted!",
		},
		{
			name:      "a role is pinged before a plural message",
			notifType: NotificationTypeTour,
			items:     2,
			roleID:    &roleID,
			want:      "<@&987654321>, 2 new tour dates announced! 🎤",
		},
		// Zero items takes the plural branch; Send only builds a header when
		// there is something to announce, so this records the shape rather than
		// endorsing it.
		{
			name:      "no items uses the plural form",
			notifType: NotificationTypeYoutube,
			items:     0,
			want:      "0 new videos posted!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bn := &BatchNotifier{
				NotificationType: tt.notifType,
				Items:            make([]NotificationItem, tt.items),
			}

			if got := bn.buildHeaderContent(tt.roleID); got != tt.want {
				t.Errorf("buildHeaderContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBatchNotifier_AddItem(t *testing.T) {
	t.Parallel()

	bn := NewBatchNotifier(nil, nil, NotificationTypeYoutube)
	if len(bn.Items) != 0 {
		t.Fatalf("a new notifier has %d items, want 0", len(bn.Items))
	}

	bn.AddItem(NotificationItem{Content: "first"})
	bn.AddItem(NotificationItem{Content: "second"})

	if len(bn.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(bn.Items))
	}
	if bn.Items[0].Content != "first" || bn.Items[1].Content != "second" {
		t.Errorf("items are out of order: %q, %q", bn.Items[0].Content, bn.Items[1].Content)
	}
}
