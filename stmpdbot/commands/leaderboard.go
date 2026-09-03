package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// The choice Values are slugs, not display names. Component custom IDs are
// routed by handler.Mux as slash-separated paths, so a value like
// "In Hand Coins" cannot survive a round trip through a pagination button.
// Only Name is ever shown to a user.
const (
	categoryCoins    = "coins"
	categoryLevels   = "levels"
	categoryMessages = "messages"
	categoryInHand   = "inhand"
)

var leaderboard = discord.SlashCommandCreate{
	Name:        "leaderboard",
	Description: "Get the leaderboard for a specific category.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:        "category",
			Description: "The category of the leaderboard.",
			Required:    true,
			Choices: []discord.ApplicationCommandOptionChoiceString{
				{Name: "Coins", Value: categoryCoins},
				{Name: "Levels", Value: categoryLevels},
				{Name: "Messages", Value: categoryMessages},
				{Name: "In Hand Coins", Value: categoryInHand},
			},
		},
	},
}

func leaderboardTitle(category string) string {
	switch category {
	case categoryCoins:
		return "Coins"
	case categoryLevels:
		return "Levels"
	case categoryMessages:
		return "Messages"
	case categoryInHand:
		return "In Hand Coins"
	}
	return "Leaderboard"
}

// leaderboardEntry is one row, already reduced to what the embed needs.
type leaderboardEntry struct {
	id    int64
	value string
}

// fetchLeaderboardPage reads one page of a category.
//
// The four categories used to be four near-identical 45-line blocks, which is
// how the same member-resolution bug came to exist four times over.
func fetchLeaderboardPage(ctx context.Context, b *stmpdbot.STMPDBot, guildID snowflake.ID, category string, page int) ([]leaderboardEntry, error) {
	offset := int32((page - 1) * utils.LeaderboardPerPage)
	limit := int32(utils.LeaderboardPerPage)
	gid := int64(guildID)

	var entries []leaderboardEntry

	switch category {
	case categoryCoins:
		records, err := b.Queries.GetCoinsLeaderboard(ctx, db.GetCoinsLeaderboardParams{
			GuildID: gid, Offset: offset, Limit: limit,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			entries = append(entries, leaderboardEntry{r.ID, strconv.FormatInt(r.StmpdCoins+r.InHand, 10)})
		}

	case categoryLevels:
		records, err := b.Queries.GetLevelsLeaderboard(ctx, db.GetLevelsLeaderboardParams{
			GuildID: gid, Offset: offset, Limit: limit,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			entries = append(entries, leaderboardEntry{r.ID, strconv.Itoa(utils.GetUserLevel(r.TotalXp))})
		}

	case categoryMessages:
		records, err := b.Queries.GetMessagesSentLeaderboard(ctx, db.GetMessagesSentLeaderboardParams{
			GuildID: gid, Offset: offset, Limit: limit,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			entries = append(entries, leaderboardEntry{r.ID, strconv.Itoa(int(r.MessagesSent))})
		}

	case categoryInHand:
		records, err := b.Queries.GetInHandLeaderboard(ctx, db.GetInHandLeaderboardParams{
			GuildID: gid, Offset: offset, Limit: limit,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			entries = append(entries, leaderboardEntry{r.ID, strconv.FormatInt(r.InHand, 10)})
		}
	}

	return entries, nil
}

// resolveLeaderboardName never returns an error, and that is the whole point.
//
// The top of every leaderboard is dominated by members who were last active in
// 2022-2025, many of whom have since left the guild. GetMember 404s for each of
// them, and the old code returned that error from inside the render loop -- so a
// single departed member emptied the entire leaderboard for everyone.
//
// A name that cannot be resolved is a cosmetic loss; the rank and the number are
// what the command is for and they are already in hand. Cache first, then REST,
// per AGENTS.md.
func resolveLeaderboardName(b *stmpdbot.STMPDBot, guildID snowflake.ID, userID snowflake.ID) (name string, present bool) {
	if member, ok := b.Client.Caches.Member(guildID, userID); ok {
		return member.EffectiveName(), true
	}
	if member, err := b.Client.Rest.GetMember(guildID, userID); err == nil && member != nil {
		return member.EffectiveName(), true
	}
	if user, err := b.Client.Rest.GetUser(userID); err == nil && user != nil {
		return user.Username, false
	}
	return userID.String(), false
}

// buildLeaderboardEmbed renders a page.
//
// Numbering is offset-based. `idx+1` was only ever correct because the offset was
// hardcoded to zero; left alone it would restart at "1." on every page.
//
// A member who has left is named rather than mentioned: <@id> for a non-member
// renders as @unknown-user in most clients, which reads as a bug in the bot.
func buildLeaderboardEmbed(b *stmpdbot.STMPDBot, guildID snowflake.ID, category string, entries []leaderboardEntry, page, totalPages int) discord.Embed {
	offset := (page - 1) * utils.LeaderboardPerPage

	var description []string
	for idx, entry := range entries {
		userID := snowflake.ID(entry.id)
		name, present := resolveLeaderboardName(b, guildID, userID)

		who := discord.UserMention(userID)
		if !present {
			who = fmt.Sprintf("%s (left)", name)
		}

		description = append(description,
			fmt.Sprintf("`%2d.` %s — %s", offset+idx+1, who, entry.value))
	}

	if len(description) == 0 {
		description = append(description, "Nothing here yet.")
	}

	embed := discord.NewEmbed().
		WithTitle(leaderboardTitle(category) + " Leaderboard").
		WithDescription(strings.Join(description, "\n")).
		WithColor(utils.ColorSuccess)

	if totalPages > 1 {
		embed = embed.WithFooter(fmt.Sprintf("Page %d of %d", page, totalPages), "")
	}

	return embed
}

func leaderboardTotalPages(ctx context.Context, b *stmpdbot.STMPDBot, guildID snowflake.ID) (int, error) {
	total, err := b.Queries.GetLeaderboardCount(ctx, int64(guildID))
	if err != nil {
		return 0, err
	}
	totalPages := utils.CalculateTotalPages(int(total), utils.LeaderboardPerPage)
	if totalPages < 1 {
		totalPages = 1
	}
	return totalPages, nil
}

func LeaderboardHandler(b *stmpdbot.STMPDBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		e.DeferCreateMessage(false)

		category := e.SlashCommandInteractionData().String("category")
		guildID := *e.GuildID()

		totalPages, err := leaderboardTotalPages(e.Ctx, b, guildID)
		if err != nil {
			return err
		}

		entries, err := fetchLeaderboardPage(e.Ctx, b, guildID, category, 1)
		if err != nil {
			return err
		}

		_, err = e.UpdateInteractionResponse(discord.NewMessageUpdate().
			WithEmbeds(buildLeaderboardEmbed(b, guildID, category, entries, 1, totalPages)).
			WithComponents(utils.CreatePaginationButtons(1, totalPages, "/leaderboard/"+category)...),
		)
		return err
	}
}

// LeaderboardPaginationHandler backs the navigation buttons. The leaderboard is
// public, so unlike the modlogs equivalent there is no permission to re-check.
func LeaderboardPaginationHandler(b *stmpdbot.STMPDBot) handler.ComponentHandler {
	return func(e *handler.ComponentEvent) error {
		// The page counter in the middle is a disabled no-op button.
		if e.Vars["action"] == "current" {
			return e.DeferUpdateMessage()
		}

		category := e.Vars["category"]
		currentPage, err := strconv.Atoi(e.Vars["page"])
		if err != nil {
			return err
		}
		guildID := *e.GuildID()

		totalPages, err := leaderboardTotalPages(e.Ctx, b, guildID)
		if err != nil {
			return err
		}

		page := currentPage
		switch e.Vars["action"] {
		case "first":
			page = 1
		case "prev":
			page = currentPage - 1
		case "next":
			page = currentPage + 1
		case "last":
			page = totalPages
		}
		page = min(max(page, 1), totalPages)

		entries, err := fetchLeaderboardPage(e.Ctx, b, guildID, category, page)
		if err != nil {
			return err
		}

		return e.UpdateMessage(discord.NewMessageUpdate().
			WithEmbeds(buildLeaderboardEmbed(b, guildID, category, entries, page, totalPages)).
			WithComponents(utils.CreatePaginationButtons(page, totalPages, "/leaderboard/"+category)...),
		)
	}
}
