package listeners

import (
	"context"
	"testing"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
)

func strPtr(s string) *string { return &s }

// TestResolveMentions covers the bug from the "cant tell who that ping is
// bro" incident: the model was seeing raw <@id> tokens with nothing to
// resolve them against. Nickname beats global name beats username, matching
// what Discord's own client displays for a mention. All three mentioned
// users are cache hits here -- displayName's REST fallback for a cache miss
// (the actual "Sourav (WEIGHTLESS Ambassador)" -> "Garrixer" incident, where
// the mentioned member simply wasn't in the member cache) is a thin,
// direct wrap of the same disgo rest.GetMember call proven out by
// buildHistory's GetMessage fallback just above it, and isn't separately
// mocked here.
func TestResolveMentions(t *testing.T) {
	guildID := snowflake.ID(1)

	nicked := discord.User{ID: 100, Username: "sourav_raw", GlobalName: strPtr("Sourav G")}
	globalOnly := discord.User{ID: 200, Username: "raw_username", GlobalName: strPtr("Global Name")}
	usernameOnly := discord.User{ID: 300, Username: "plain_user"}

	caches := cache.New(cache.WithCaches(cache.FlagMembers))
	caches.AddMember(discord.Member{User: nicked, Nick: strPtr("Sourav (WEIGHTLESS Ambassador)"), GuildID: guildID})
	caches.AddMember(discord.Member{User: globalOnly, GuildID: guildID})   // cached, no nick set
	caches.AddMember(discord.Member{User: usernameOnly, GuildID: guildID}) // cached, no nick set

	b := &stmpdbot.STMPDBot{Client: &bot.Client{Caches: caches}}

	content := "what you think about <@100> ? also <@200> and <@300> and unknown <@999>"
	mentions := []discord.User{nicked, globalOnly, usernameOnly}

	got := resolveMentions(context.Background(), b, guildID, content, mentions)
	want := "what you think about @Sourav (WEIGHTLESS Ambassador) ? also @Global Name and @plain_user and unknown <@999>"

	if got != want {
		t.Errorf("resolveMentions() =\n  %q\nwant:\n  %q", got, want)
	}
}

func TestResolveMentions_NoMentionsIsUntouched(t *testing.T) {
	b := &stmpdbot.STMPDBot{Client: &bot.Client{Caches: cache.New()}}
	content := "no mentions here at all"

	if got := resolveMentions(context.Background(), b, 1, content, nil); got != content {
		t.Errorf("resolveMentions() = %q, want unchanged %q", got, content)
	}
}
