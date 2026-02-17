package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/gocolly/colly/v2"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func GetAllTourShows(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	for ; ; <-ticker.C {
		slog.Info("Running tour shows fetcher")

		err := b.Collector.Visit("https://martingarrix.com/tour/")
		if err != nil {
			slog.Error("Failed to visit martingarrix.com/tour", slog.Any("err", err))
			continue
		}

		var shows []utils.TourShow

		b.Collector.OnHTML(".schedule-items_schedule-item__nFRn0", func(e *colly.HTMLElement) {
			var show utils.TourShow

			// Extract date (e.g., "Feb 28, 2026")
			dateStr := e.ChildText(".schedule-items_schedule-item-date__3GjPi")
			if dateStr != "" {
				// Parse the date string
				parsedDate, err := time.Parse("Jan 2, 2006", dateStr)
				if err != nil {
					slog.Warn("Failed to parse date", slog.String("date", dateStr), slog.Any("err", err))
					return
				}
				show.ShowDate = parsedDate
			}

			// Extract show name (e.g., "OMNIA Nightclub")
			show.ShowName = strings.TrimSpace(e.ChildText(".schedule-items_schedule-item-title__Vt1s7"))

			// Extract location (e.g., "Las Vegas, United States of America")
			locationStr := e.ChildText(".schedule-items_schedule-item-location__gh0B3")
			if locationStr != "" {
				// Split location into city and country
				parts := strings.Split(locationStr, ",")
				if len(parts) >= 2 {
					show.City = strings.TrimSpace(parts[0])
					// Join remaining parts as country (handles cases like "United States of America")
					show.Country = strings.TrimSpace(strings.Join(parts[1:], ","))
				} else if len(parts) == 1 {
					// If no comma, use the whole string as city
					show.City = strings.TrimSpace(parts[0])
					show.Country = "TBA"
				}
			} else {
				// Fallback if no location found
				show.City = "TBA"
				show.Country = "TBA"
			}

			// Extract venue (optional - some shows don't have a specific venue)
			show.Venue = strings.TrimSpace(e.ChildText(".schedule-items_schedule-item-venue__7hq84"))
			if show.Venue == "" {
				show.Venue = "Venue TBA"
			}

			// Extract ticket URL
			ticketLink := e.ChildAttr(".schedule-items_schedule-item-link__Sl_da", "href")
			if ticketLink != "" {
				show.TicketURL = ticketLink
			}

			// Only add shows with valid required fields (venue is optional)
			if show.ShowName == "" || show.ShowDate.IsZero() || show.City == "" || show.Country == "" {
				slog.Warn("Skipping show with missing critical fields",
					slog.String("show_name", show.ShowName),
					slog.String("city", show.City),
					slog.String("country", show.Country),
					slog.Bool("has_date", !show.ShowDate.IsZero()))
				return
			}
			shows = append(shows, show)
		})

		b.Collector.Wait()

		if len(shows) == 0 {
			slog.Info("No tour shows found")
			continue
		}

		slog.Info(fmt.Sprintf("Found %d tour shows on website", len(shows)))

		// Create a batch notifier for this cycle
		notifier := utils.NewBatchNotifier(b.Queries, b.Client.Rest(), utils.NotificationTypeTour)

		for _, show := range shows {
			// Check if show already exists
			doesExist, err := b.Queries.DoesTourShowExist(context.Background(), db.DoesTourShowExistParams{
				ShowName: show.ShowName,
				ShowDate: pgtype.Date{Time: show.ShowDate, Valid: true},
				Venue:    show.Venue,
			})

			if err != nil {
				slog.Error("Failed to check if tour show exists", slog.Any("err", err))
				continue
			}

			if doesExist {
				continue
			}

			// Prepare insert parameters
			showParams := db.InsertTourShowParams{
				ShowName: show.ShowName,
				City:     show.City,
				Country:  show.Country,
				Venue:    show.Venue,
				ShowDate: pgtype.Date{Time: show.ShowDate, Valid: true},
			}

			if show.TicketURL != "" {
				showParams.TicketUrl = pgtype.Text{
					String: show.TicketURL,
					Valid:  true,
				}
			}

			// Insert to database
			insertedShow, err := b.Queries.InsertTourShow(context.Background(), showParams)
			if err != nil {
				slog.Error("Failed to insert tour show for "+show.ShowName, slog.Any("err", err))
				continue
			}

			// Create announcement embed
			// Validate show name is not empty
			if show.ShowName == "" {
				slog.Error("Show name is empty, skipping embed creation")
				continue
			}

			// Format the description with better layout
			description := fmt.Sprintf("**%s**\n%s, %s\n\n📅 %s",
				show.Venue,
				show.City,
				show.Country,
				show.ShowDate.Format("Monday, January 2, 2006"))

			// Validate description is not too long (Discord limit is 4096)
			if len(description) > 4096 {
				description = description[:4093] + "..."
			}

			embedBuilder := discord.NewEmbedBuilder().
				SetTitle(show.ShowName).
				SetDescription(description).
				SetColor(0xFFA500). // Brighter orange color
				SetTimestamp(time.Now())

			announcementEmbed := embedBuilder.Build()

			// Prepare components (ticket button if available)
			var components []discord.ContainerComponent
			if insertedShow.TicketUrl.Valid && insertedShow.TicketUrl.String != "" {
				ticketURL := insertedShow.TicketUrl.String

				// Ensure URL has a valid scheme (Discord requires http:// or https://)
				if !strings.HasPrefix(ticketURL, "http://") && !strings.HasPrefix(ticketURL, "https://") {
					ticketURL = "https://" + ticketURL
				}

				components = []discord.ContainerComponent{
					discord.NewActionRow(
						discord.NewLinkButton("🎟️ Get Tickets", ticketURL),
					),
				}
			}

			// Add this show to the batch
			notifier.AddItem(utils.NotificationItem{
				Embed:      &announcementEmbed,
				Components: components,
			})

			slog.Info(fmt.Sprintf("Added new tour show: %s on %s", show.ShowName, show.ShowDate.Format("Jan 2, 2006")))
		}

		// Send all batched notifications once
		if err := notifier.Send(); err != nil {
			slog.Error("Failed to send batched tour notifications", slog.Any("err", err))
		}
	}
}
