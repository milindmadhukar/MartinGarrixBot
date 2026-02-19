package utils

import (
	"fmt"
	"math"

	"github.com/disgoorg/disgo/discord"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

const ModlogsPerPage = 5

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
	eb := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("Moderation Logs for <@%d>", userID)).
		SetColor(ColorInfo).
		SetFooter(fmt.Sprintf("Page %d of %d", page, totalPages), "")

	if len(logs) == 0 {
		eb.SetDescription("No moderation logs found for this user.")
		return eb.Build()
	}

	description := ""
	startIndex := (page - 1) * ModlogsPerPage
	for i, log := range logs {
		if i > 0 {
			description += "\n\n"
		}
		description += FormatModlogEntry(log, startIndex+i+1)
	}

	eb.SetDescription(description)
	return eb.Build()
}

// CalculateTotalPages calculates the total number of pages for pagination
func CalculateTotalPages(totalItems int, itemsPerPage int) int {
	return int(math.Ceil(float64(totalItems) / float64(itemsPerPage)))
}

// CreatePaginationButtons creates the navigation buttons for pagination
func CreatePaginationButtons(currentPage, totalPages int, customID string) []discord.ContainerComponent {
	if totalPages <= 1 {
		return []discord.ContainerComponent{}
	}

	return []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀◀", fmt.Sprintf("%s:first:%d", customID, currentPage)).
				WithDisabled(currentPage == 1),
			discord.NewSecondaryButton("◀", fmt.Sprintf("%s:prev:%d", customID, currentPage)).
				WithDisabled(currentPage == 1),
			discord.NewSecondaryButton(fmt.Sprintf("%d / %d", currentPage, totalPages), fmt.Sprintf("%s:current:%d", customID, currentPage)).
				WithDisabled(true),
			discord.NewSecondaryButton("▶", fmt.Sprintf("%s:next:%d", customID, currentPage)).
				WithDisabled(currentPage == totalPages),
			discord.NewSecondaryButton("▶▶", fmt.Sprintf("%s:last:%d", customID, currentPage)).
				WithDisabled(currentPage == totalPages),
		),
	}
}
