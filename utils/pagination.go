package utils

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

const ModlogsPerPage = 5

// LeaderboardPerPage is the page size for /leaderboard.
const LeaderboardPerPage = 10

// FormatModlogEntry formats a single modlog entry for display
func FormatModlogEntry(log db.Modlog, index int) string {
	reason := "No reason provided"
	if log.Reason.Valid {
		reason = log.Reason.String
	}

	timeStr := "Unknown"
	if log.Time.Valid {
		timeStr = fmt.Sprintf("<t:%d:F>", log.Time.Time.Unix())
	}

	entry := fmt.Sprintf("**%d.** %s | Case #%d\n", index, log.LogType, log.ID)
	entry += fmt.Sprintf("• Moderator: <@%d>\n", log.ModeratorID)
	entry += fmt.Sprintf("• Reason: %s\n", reason)
	entry += fmt.Sprintf("• Time: %s", timeStr)

	if log.ExpiresAt.Valid {
		expiresStr := fmt.Sprintf("<t:%d:R>", log.ExpiresAt.Time.Unix())
		if log.Active.Valid && log.Active.Bool {
			entry += fmt.Sprintf("\n• Expires: %s", expiresStr)
		} else {
			entry += "\n• Status: Expired/Deactivated"
		}
	}

	return entry
}

// CreateModlogEmbed creates an embed for displaying modlogs
func CreateModlogEmbed(logs []db.Modlog, userID int64, page, totalPages int) discord.Embed {
	eb := discord.NewEmbed().
		WithTitle(fmt.Sprintf("Moderation Logs for <@%d>", userID)).
		WithColor(ColorInfo).
		WithFooter(fmt.Sprintf("Page %d of %d", page, totalPages), "")

	if len(logs) == 0 {
		eb = eb.WithDescription("No moderation logs found for this user.")
		return eb
	}

	description := ""
	startIndex := (page - 1) * ModlogsPerPage
	for i, log := range logs {
		if i > 0 {
			description += "\n\n"
		}
		description += FormatModlogEntry(log, startIndex+i+1)
	}

	eb = eb.WithDescription(description)
	return eb
}

// CalculateTotalPages calculates the total number of pages for pagination.
// A non-positive page size has no meaningful answer, so it reports zero pages
// rather than dividing by zero: the float form used to yield int(+Inf), which
// the Go spec leaves undefined.
func CalculateTotalPages(totalItems int, itemsPerPage int) int {
	if itemsPerPage <= 0 || totalItems <= 0 {
		return 0
	}
	return (totalItems + itemsPerPage - 1) / itemsPerPage
}

// CreatePaginationButtons creates the navigation buttons for pagination.
//
// customID must be a router path such as "/modlogs/123", because disgo's
// handler.Mux matches component custom IDs as slash-separated patterns. The
// action and the current page are appended as two further path segments.
func CreatePaginationButtons(currentPage, totalPages int, customID string) []discord.LayoutComponent {
	if totalPages <= 1 {
		return []discord.LayoutComponent{}
	}

	id := func(action string) string {
		return fmt.Sprintf("%s/%s/%d", customID, action, currentPage)
	}

	return []discord.LayoutComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀◀", id("first")).
				WithDisabled(currentPage == 1),
			discord.NewSecondaryButton("◀", id("prev")).
				WithDisabled(currentPage == 1),
			discord.NewSecondaryButton(fmt.Sprintf("%d / %d", currentPage, totalPages), id("current")).
				WithDisabled(true),
			discord.NewSecondaryButton("▶", id("next")).
				WithDisabled(currentPage == totalPages),
			discord.NewSecondaryButton("▶▶", id("last")).
				WithDisabled(currentPage == totalPages),
		),
	}
}
