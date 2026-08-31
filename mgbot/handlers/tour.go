package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

const tourURL = "https://martingarrix.com/tour/"

// tourClient bounds the request. Like the STMPD fetcher, this no longer uses the
// shared colly collector: martingarrix.com is a Next.js site that ships its tour
// data as JSON, and the CSS-module class names the old scrape targeted are build
// artefacts that change on every deploy of theirs.
var tourClient = &http.Client{Timeout: 30 * time.Second}

// prismicRichText is Prismic's rich-text shape: a list of blocks, each carrying
// the plain text of one paragraph or heading.
type prismicRichText []struct {
	Text string `json:"text"`
}

func (p prismicRichText) String() string {
	parts := make([]string, 0, len(p))
	for _, block := range p {
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

// tourNextData is the slice of the __NEXT_DATA__ blob that carries the shows.
type tourNextData struct {
	Props struct {
		PageProps struct {
			ToursData []struct {
				Data struct {
					Announced  bool            `json:"announced"`
					Date       string          `json:"date"`
					Title      prismicRichText `json:"title"`
					Venue      prismicRichText `json:"venue"`
					Location   prismicRichText `json:"location"`
					TicketLink struct {
						URL string `json:"url"`
					} `json:"ticket_link"`
				} `json:"data"`
			} `json:"toursData"`
		} `json:"pageProps"`
	} `json:"props"`
}

// nextDataPayload pulls the JSON out of Next.js's <script id="__NEXT_DATA__">.
func nextDataPayload(body string) (string, error) {
	const marker = `id="__NEXT_DATA__"`

	i := strings.Index(body, marker)
	if i < 0 {
		return "", fmt.Errorf("no __NEXT_DATA__ script found")
	}

	open := strings.Index(body[i:], ">")
	if open < 0 {
		return "", fmt.Errorf("malformed __NEXT_DATA__ script tag")
	}
	start := i + open + 1

	end := strings.Index(body[start:], "</script>")
	if end < 0 {
		return "", fmt.Errorf("unterminated __NEXT_DATA__ script tag")
	}

	return body[start : start+end], nil
}

// discordMaxButtonURL is Discord's limit for a link button's url field. Exceeding
// it fails the whole message with "50035: Invalid Form Body", not just the button.
const discordMaxButtonURL = 512

// sanitizeTicketURL drops the Google Analytics cross-domain linker parameters
// that ticket vendors append. They are not needed to open the page, and
// taogroup's push some URLs past 650 characters, over Discord's button limit.
func sanitizeTicketURL(raw string) string {
	if raw == "" {
		return ""
	}

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	query := u.Query()
	for key := range query {
		// _gl, _ga, _ga_*, _gcl_aw, _gcl_au, _fplc and FPAU are all analytics.
		if strings.HasPrefix(key, "_") || key == "FPAU" {
			query.Del(key)
		}
	}
	u.RawQuery = query.Encode()

	return u.String()
}

// fetchTourShows returns the announced shows, ordered by date.
func fetchTourShows() ([]utils.TourShow, error) {
	req, err := http.NewRequest("GET", tourURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tour request: %w", err)
	}
	req.Header.Set("User-Agent", "MartinGarrixBot (+https://github.com/milindmadhukar/MartinGarrixBot)")

	resp, err := tourClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tour page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tour page returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read tour page body: %w", err)
	}

	payload, err := nextDataPayload(string(body))
	if err != nil {
		return nil, fmt.Errorf("failed to locate tour data: %w", err)
	}

	var parsed tourNextData
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode tour data: %w", err)
	}

	docs := parsed.Props.PageProps.ToursData
	shows := make([]utils.TourShow, 0, len(docs))

	for _, doc := range docs {
		d := doc.Data

		// Unannounced dates are placeholders on the site; announcing them would
		// leak a show before the artist does.
		if !d.Announced {
			continue
		}

		showDate, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			slog.Warn("Skipping tour show with unparseable date",
				slog.String("date", d.Date), slog.Any("err", err))
			continue
		}

		show := utils.TourShow{
			ShowName:  d.Title.String(),
			Venue:     d.Venue.String(),
			ShowDate:  showDate,
			TicketURL: sanitizeTicketURL(d.TicketLink.URL),
		}

		// Location reads "City, Country"; anything after the first comma is the
		// country, so names like "United States of America" survive intact.
		if location := d.Location.String(); location != "" {
			if city, country, found := strings.Cut(location, ","); found {
				show.City = strings.TrimSpace(city)
				show.Country = strings.TrimSpace(country)
			} else {
				show.City = strings.TrimSpace(location)
				show.Country = "TBA"
			}
		} else {
			show.City = "TBA"
			show.Country = "TBA"
		}

		if show.Venue == "" {
			show.Venue = "Venue TBA"
		}

		if show.ShowName == "" || show.City == "" || show.Country == "" {
			slog.Warn("Skipping tour show with missing critical fields",
				slog.String("show_name", show.ShowName),
				slog.String("city", show.City),
				slog.String("country", show.Country))
			continue
		}

		shows = append(shows, show)
	}

	slices.SortFunc(shows, func(a, b utils.TourShow) int {
		return a.ShowDate.Compare(b.ShowDate)
	})

	return shows, nil
}

func GetAllTourShows(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	for ; ; <-ticker.C {
		slog.Info("Running tour shows fetcher")

		shows, err := fetchTourShows()
		if err != nil {
			slog.Error("Failed to fetch tour shows", slog.Any("err", err))
			utils.RecordSourceFailure(utils.SourceTour, err)
			continue
		}

		// An empty tour page is genuinely normal between tour announcements, so
		// unlike the release feeds this is a success with nothing in it.
		utils.RecordSourceSuccess(utils.SourceTour)

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

				// Drop the button rather than lose the whole announcement to it.
				if len(ticketURL) > discordMaxButtonURL {
					slog.Warn("Ticket URL too long for a link button, announcing without it",
						slog.String("show_name", show.ShowName),
						slog.Int("url_length", len(ticketURL)))
				} else {
					components = []discord.ContainerComponent{
						discord.NewActionRow(
							discord.NewLinkButton("🎟️ Get Tickets", ticketURL),
						),
					}
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
