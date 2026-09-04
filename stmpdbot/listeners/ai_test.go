package listeners

import (
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
// what Discord's own client displays for a mention.
func TestResolveMentions(t *testing.T) {
	guildID := snowflake.ID(1)

	nicked := discord.User{ID: 100, Username: "sourav_raw", GlobalName: strPtr("Sourav G")}
	globalOnly := discord.User{ID: 200, Username: "raw_username", GlobalName: strPtr("Global Name")} // not in member cache
	usernameOnly := discord.User{ID: 300, Username: "plain_user"}                                    // not in member cache, no global name

	caches := cache.New(cache.WithCaches(cache.FlagMembers))
	caches.AddMember(discord.Member{User: nicked, Nick: strPtr("Sourav (WEIGHTLESS Ambassador)"), GuildID: guildID})

	b := &stmpdbot.STMPDBot{Client: &bot.Client{Caches: caches}}

	content := "what you think about <@100> ? also <@200> and <@300> and unknown <@999>"
	mentions := []discord.User{nicked, globalOnly, usernameOnly}

	got := resolveMentions(b, guildID, content, mentions)
	want := "what you think about @Sourav (WEIGHTLESS Ambassador) ? also @Global Name and @plain_user and unknown <@999>"

	if got != want {
		t.Errorf("resolveMentions() =\n  %q\nwant:\n  %q", got, want)
	}
}

func TestResolveMentions_NoMentionsIsUntouched(t *testing.T) {
	b := &stmpdbot.STMPDBot{Client: &bot.Client{Caches: cache.New()}}
	content := "no mentions here at all"

	if got := resolveMentions(b, 1, content, nil); got != content {
		t.Errorf("resolveMentions() = %q, want unchanged %q", got, content)
	}
}
