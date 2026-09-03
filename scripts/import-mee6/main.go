// Command import-mee6 replaces this bot's XP and message counts with Mee6's.
//
// Mee6 has run alongside this bot in the STMPD server the whole time and kept
// counting correctly while our own numbers drifted: its XP total for the server
// is roughly three and a half times ours, and our messages_sent is inflated for
// about half the members -- one member reads 46,703 here against 11,942 there.
// The drift came from a NULL total_xp restarting a member's XP from zero, an
// absolute XP write that could lose an award, and a counter that moved on
// redelivered gateway events.
//
// The two systems use the SAME level curve -- Mee6's per-level cost is
// 5*lvl^2 + 50*lvl + 100, which is exactly utils.FXpForNextLevel, because this
// bot's Python predecessor copied it. So the XP numbers are directly
// comparable and no conversion is applied; after this runs, /rank and
// /leaderboard agree with mee6.xyz/leaderboard/<guild> exactly.
//
// This OVERWRITES. Members whose Mee6 XP is lower than ours lose the
// difference, which is the point: Mee6 is being treated as the source of truth.
// Members absent from Mee6 are left alone rather than zeroed. Coins are never
// touched -- they are this bot's own economy.
//
// Always dry-run first and read the summary:
//
//	go run ./scripts/import-mee6 -config config.prod.toml -guild <id> -dry-run
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// mee6PageSize is the largest page the API serves. Fewer, larger pages means
// fewer requests at somebody else's expense.
const mee6PageSize = 1000

// maxPages bounds the walk so a misbehaving API cannot spin forever. At 1000 a
// page this covers 20,000 members, well past any guild this bot is in.
const maxPages = 20

type mee6Player struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	XP           int    `json:"xp"`
	MessageCount int    `json:"message_count"`
	Level        int    `json:"level"`
}

type mee6Page struct {
	Players []mee6Player `json:"players"`
	// XPPerMessage and XPRate are read only to be checked: if Mee6's rates ever
	// stop matching ours, the imported numbers would not mean the same thing.
	XPPerMessage []int   `json:"xp_per_message"`
	XPRate       float64 `json:"xp_rate"`
}

func fetchPage(ctx context.Context, client *http.Client, guildID string, page int) (mee6Page, error) {
	url := fmt.Sprintf(
		"https://mee6.xyz/api/plugins/levels/leaderboard/%s?limit=%d&page=%d",
		guildID, mee6PageSize, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return mee6Page{}, err
	}
	// The API 403s the default Go user agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; STMPDBot import-mee6)")

	resp, err := client.Do(req)
	if err != nil {
		return mee6Page{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return mee6Page{}, fmt.Errorf("mee6 returned %s for page %d", resp.Status, page)
	}

	var out mee6Page
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return mee6Page{}, fmt.Errorf("decoding page %d: %w", page, err)
	}
	return out, nil
}

func main() {
	// Registered before Setup, which is what calls flag.Parse.
	guildFlag := flag.String("guild", "690950056202731521", "guild id to import")

	env, ctx, cleanup := script.Setup("import-mee6")
	defer cleanup()

	guildID, err := strconv.ParseInt(*guildFlag, 10, 64)
	if err != nil {
		script.Fatal("invalid guild id", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	var players []mee6Player
	for page := range maxPages {
		p, err := fetchPage(ctx, client, *guildFlag, page)
		if err != nil {
			script.Fatal("failed to fetch the mee6 leaderboard", err)
		}

		if page == 0 {
			slog.Info("Mee6 rates",
				slog.Any("xp_per_message", p.XPPerMessage),
				slog.Float64("xp_rate", p.XPRate))
			// Our own award is 15-25 at a 1x multiplier. A mismatch does not stop
			// the import -- the totals are still Mee6's truth -- but it means new
			// XP will accrue at a different rate than the imported history.
			if len(p.XPPerMessage) == 2 && (p.XPPerMessage[0] != 15 || p.XPPerMessage[1] != 25) {
				slog.Warn("Mee6 awards a different XP range than this bot",
					slog.Any("mee6", p.XPPerMessage), slog.String("ours", "[15 25]"))
			}
		}

		players = append(players, p.Players...)
		if len(p.Players) < mee6PageSize {
			break
		}
		// Paced so a full walk is a handful of requests spread over a second or
		// two rather than a burst.
		time.Sleep(250 * time.Millisecond)
	}

	slog.Info("Fetched the mee6 leaderboard", slog.Int("players", len(players)))
	if len(players) == 0 {
		script.Fatal("mee6 returned no players", fmt.Errorf("guild %d", guildID))
	}

	existing := map[int64]db.User{}
	rows, err := env.Queries.GetUsersInGuild(ctx, guildID)
	if err != nil {
		script.Fatal("failed to read the current users", err)
	}
	for _, u := range rows {
		existing[u.ID] = u
	}
	slog.Info("Loaded current rows", slog.Int("count", len(existing)))

	var inserted, xpUp, xpDown, unchanged, msgDown int
	var xpBefore, xpAfter int64

	progress := script.NewProgress("import-mee6", len(players))
	for _, p := range players {
		progress.Step()

		id, err := strconv.ParseInt(p.ID, 10, 64)
		if err != nil {
			slog.Warn("Skipping an unparseable member id", slog.String("id", p.ID))
			continue
		}

		current, seen := existing[id]
		if !seen {
			inserted++
		} else {
			xpBefore += int64(current.TotalXp)
			switch {
			case int32(p.XP) > current.TotalXp:
				xpUp++
			case int32(p.XP) < current.TotalXp:
				xpDown++
				slog.Debug("XP drops for this member",
					slog.String("user", p.Username),
					slog.Int("from", int(current.TotalXp)), slog.Int("to", p.XP))
			default:
				unchanged++
			}
			if int32(p.MessageCount) < current.MessagesSent {
				msgDown++
			}
		}
		xpAfter += int64(p.XP)

		// Mee6's own level is not stored: this schema has no level column and
		// derives it from total_xp. Since the curves match, the derived level
		// equals Mee6's -- assert that rather than assume it.
		if got := utils.GetUserLevel(int32(p.XP)); got != p.Level {
			slog.Warn("Level curve disagrees with mee6",
				slog.String("user", p.Username), slog.Int("xp", p.XP),
				slog.Int("ours", got), slog.Int("mee6", p.Level))
		}

		// A dry run reports and writes nothing at all, rather than writing
		// inside script.Setup's roll-back transaction. That transaction would
		// hold a row lock on every member for the whole pass -- minutes over the
		// WAN link to the VPS -- and the live bot's MessageSent queues behind it.
		// The first attempt at this blocked the running bot for 21 seconds. For
		// a plain overwrite there is nothing a trial write would reveal that the
		// diff above does not already say.
		if env.DryRun {
			continue
		}

		if err := env.Queries.ImportUserStats(ctx, db.ImportUserStatsParams{
			ID:           id,
			GuildID:      guildID,
			TotalXp:      int32(p.XP),
			MessagesSent: int32(p.MessageCount),
		}); err != nil {
			script.Fatal("failed to import a member", err)
		}
	}
	progress.Done()

	slog.Info("Import summary",
		slog.Int("members_imported", len(players)),
		slog.Int("inserted_new", inserted),
		slog.Int("xp_raised", xpUp),
		slog.Int("xp_lowered", xpDown),
		slog.Int("xp_unchanged", unchanged),
		slog.Int("messages_lowered", msgDown),
		slog.Int64("xp_sum_before", xpBefore),
		slog.Int64("xp_sum_after", xpAfter),
		slog.Int("left_untouched", len(existing)-(len(players)-inserted)),
		slog.Bool("dry_run", env.DryRun))
}
