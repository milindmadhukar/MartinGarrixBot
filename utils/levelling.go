package utils

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fogleman/gg"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nfnt/resize"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

const rankCardFontPath = "assets/font.ttf"

// rankCardBackgroundsDir defaults to the images baked into the bot's own
// image, and is overridden by SetBackgroundsDir at startup when
// storage.backgrounds_dir points at a shared, writable volume instead.
var rankCardBackgroundsDir = "assets/backgrounds"

// SetBackgroundsDir points the rank card generator at a different backgrounds
// directory. Called once at startup; a blank dir leaves the default in place.
func SetBackgroundsDir(dir string) {
	if dir != "" {
		rankCardBackgroundsDir = dir
	}
}

func FXpForNextLevel(lvl int) int32 {
	return int32(5*lvl*lvl + 50*lvl + 100)
}

// GetTotalXp returns the total XP required based on the given level.
func GetTotalXp(lvl int) int32 {
	var totalSum int32 = 0
	for i := 0; i < lvl; i++ {
		totalSum += FXpForNextLevel(i)
	}
	return totalSum
}

// GetUserLevel calculates the user's level based on total XP.
func GetUserLevel(totalXp int32) int {
	lvl := 0
	var totalSum int32 = 0
	for totalSum <= totalXp {
		totalSum += FXpForNextLevel(lvl)
		lvl++
	}
	return lvl - 1
}

func GetUserLevelData(totalXp int32) UserLevelData {
	lvl := GetUserLevel(totalXp)
	return UserLevelData{
		Lvl:          lvl,
		XpForNextLvl: FXpForNextLevel(lvl),
		CurrentXp:    totalXp - GetTotalXp(lvl),
	}
}

// pickRandomBackground picks a random image out of assets/backgrounds. Adding
// a new background is just dropping a new image file in that directory.
func pickRandomBackground() (string, error) {
	entries, err := os.ReadDir(rankCardBackgroundsDir)
	if err != nil {
		return "", err
	}

	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".png", ".jpg", ".jpeg":
			candidates = append(candidates, filepath.Join(rankCardBackgroundsDir, entry.Name()))
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no background images found in %s", rankCardBackgroundsDir)
	}

	return candidates[rand.Intn(len(candidates))], nil
}

// pickBackgroundForGuild picks a background from the guild's selection in the
// backgrounds catalogue (or the whole catalogue, if the guild hasn't picked
// any), honouring the guild's random/cycle mode. Falls back to a directory
// scan if the catalogue itself is empty, which should only happen before the
// startup seed has ever run.
func pickBackgroundForGuild(ctx context.Context, queries *db.Queries, guildID int64) (string, error) {
	rows, err := queries.ListGuildBackgrounds(ctx, guildID)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		if rows, err = queries.ListBackgrounds(ctx); err != nil {
			return "", err
		}
	}
	if len(rows) == 0 {
		return pickRandomBackground()
	}

	settings, err := queries.GetGuildBackgroundSettings(ctx, guildID)
	if err != nil || settings.BackgroundMode != "cycle" {
		// No guild row yet, or plain random mode: random needs no persisted
		// state either way.
		return filepath.Join(rankCardBackgroundsDir, rows[rand.Intn(len(rows))].Filename), nil
	}

	next := nextInCycle(rows, settings.BackgroundCycleBackgroundID)
	// Best-effort: if this write fails the next render just repeats the same
	// pick, which is not worth failing the whole rank card over.
	_ = queries.SetGuildBackgroundCyclePosition(ctx, db.SetGuildBackgroundCyclePositionParams{
		GuildID:                     guildID,
		BackgroundCycleBackgroundID: pgtype.Int8{Int64: next.ID, Valid: true},
	})

	return filepath.Join(rankCardBackgroundsDir, next.Filename), nil
}

// nextInCycle advances to the background after `current` in id order,
// wrapping around. A missing or fallen-out-of-selection current starts over
// from the first background.
func nextInCycle(rows []db.Background, current pgtype.Int8) db.Background {
	if current.Valid {
		for i, row := range rows {
			if row.ID == current.Int64 {
				return rows[(i+1)%len(rows)]
			}
		}
	}
	return rows[0]
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

// coverResizeCrop resizes img so it fully covers a targetW x targetH canvas
// and center-crops the overflow, the same way CSS `background-size: cover`
// would, so a background image of any size or aspect ratio works.
func coverResizeCrop(img image.Image, targetW, targetH int) image.Image {
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	scale := math.Max(float64(targetW)/float64(srcW), float64(targetH)/float64(srcH))
	newW := int(math.Ceil(float64(srcW) * scale))
	newH := int(math.Ceil(float64(srcH) * scale))

	resized := resize.Resize(uint(newW), uint(newH), img, resize.Lanczos3)

	offsetX := (newW - targetW) / 2
	offsetY := (newH - targetH) / 2

	cropped := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	draw.Draw(cropped, cropped.Bounds(), resized, image.Pt(offsetX, offsetY), draw.Src)
	return cropped
}

// averageColor returns the mean RGB colour of every pixel in img.
func averageColor(img image.Image) color.RGBA {
	bounds := img.Bounds()
	var rSum, gSum, bSum, count uint64

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			rSum += uint64(r >> 8)
			gSum += uint64(g >> 8)
			bSum += uint64(b >> 8)
			count++
		}
	}

	if count == 0 {
		return color.RGBA{R: 93, G: 93, B: 93, A: 255}
	}

	return color.RGBA{
		R: uint8(rSum / count),
		G: uint8(gSum / count),
		B: uint8(bSum / count),
		A: 255,
	}
}

// Floors applied to the average colour's saturation/value so the progress
// fill always reads as a bright, neon accent instead of a muddy average.
const (
	neonMinSaturation = 0.75
	neonMinValue      = 0.9
)

func rgbToHSV(r, g, b uint8) (h, s, v float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	v = max
	d := max - min

	if max == 0 {
		return 0, 0, v
	}
	s = d / max

	if d == 0 {
		return 0, s, v
	}
	switch max {
	case rf:
		h = math.Mod((gf-bf)/d, 6)
	case gf:
		h = (bf-rf)/d + 2
	default:
		h = (rf-gf)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, v
}

func hsvToRGB(h, s, v float64) color.RGBA {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c

	var rf, gf, bf float64
	switch {
	case h < 60:
		rf, gf, bf = c, x, 0
	case h < 120:
		rf, gf, bf = x, c, 0
	case h < 180:
		rf, gf, bf = 0, c, x
	case h < 240:
		rf, gf, bf = 0, x, c
	case h < 300:
		rf, gf, bf = x, 0, c
	default:
		rf, gf, bf = c, 0, x
	}

	return color.RGBA{
		R: uint8((rf + m) * 255),
		G: uint8((gf + m) * 255),
		B: uint8((bf + m) * 255),
		A: 255,
	}
}

// neonAccentColor takes the background's average colour and pushes its
// saturation/brightness up to a neon floor while keeping its hue, so the
// progress fill always looks vivid and "of" the background rather than a
// dull, muddy average.
func neonAccentColor(img image.Image) color.RGBA {
	avg := averageColor(img)
	h, s, v := rgbToHSV(avg.R, avg.G, avg.B)

	if s < neonMinSaturation {
		s = neonMinSaturation
	}
	if v < neonMinValue {
		v = neonMinValue
	}

	return hsvToRGB(h, s, v)
}

// dynamicFontSize shrinks text down from maxSize until it fits within
// imgFraction of the card width, mirroring the previous shrink-long-usernames
// behaviour.
func dynamicFontSize(ctx *gg.Context, text string, maxSize int, imgFraction float64) (int, error) {
	fontSize := 12
	for {
		if err := ctx.LoadFontFace(rankCardFontPath, float64(fontSize)); err != nil {
			return 0, err
		}
		w, _ := ctx.MeasureString(text)
		if w >= float64(RANK_PICTURE_WIDTH)*imgFraction {
			break
		}
		fontSize++
		if fontSize >= maxSize {
			break
		}
	}
	return fontSize - 1, nil
}

func RankPicture(ctx context.Context, queries *db.Queries, user db.GetUserLevelDataRow, memberName string, avatarUrl string) (image.Image, error) {
	lvlData := GetUserLevelData(user.TotalXp)
	percentage := float64(lvlData.CurrentXp) / float64(lvlData.XpForNextLvl)

	bgPath, err := pickBackgroundForGuild(ctx, queries, user.GuildID)
	if err != nil {
		return nil, err
	}

	bgImg, err := loadImage(bgPath)
	if err != nil {
		return nil, err
	}
	background := coverResizeCrop(bgImg, RankCardWidth, RankCardHeight)
	fillColor := neonAccentColor(background)

	dc := gg.NewContext(RankCardWidth, RankCardHeight)
	dc.DrawImage(background, 0, 0)

	// Translucent rounded panel, inset from the canvas edges.
	panelX := float64(RankPanelInset)
	panelY := float64(RankPanelInset)
	panelW := float64(RankCardWidth - 2*RankPanelInset)
	panelH := float64(RankCardHeight - 2*RankPanelInset)
	dc.DrawRoundedRectangle(panelX, panelY, panelW, panelH, RankPanelRadius)
	dc.SetRGBA255(18, 18, 18, 145)
	dc.Fill()

	// Progress bar track (grey stadium/pill).
	barX := float64(RankBarX)
	barY := float64(RankBarY)
	barW := float64(RANK_PICTURE_WIDTH)
	barH := float64(RankBarHeight)
	dc.DrawRoundedRectangle(barX, barY, barW, barH, barH/2)
	dc.SetRGBA255(93, 93, 93, 255)
	dc.Fill()

	// Progress fill, clipped to the same pill shape so only its left cap is
	// rounded (matching how much of the bar is actually filled).
	fillWidth := barW * percentage
	if fillWidth > 0 {
		dc.Push()
		dc.DrawRoundedRectangle(barX, barY, barW, barH, barH/2)
		dc.Clip()
		dc.SetColor(fillColor)
		dc.DrawRectangle(barX, barY, fillWidth, barH)
		dc.Fill()
		dc.Pop()
		// See the matching comment above the avatar's ResetClip() call.
		dc.ResetClip()
	}

	// Avatar, circular-masked.
	pfp, err := http.Get(avatarUrl)
	if err != nil {
		return nil, err
	}
	defer pfp.Body.Close()

	avatar, _, err := image.Decode(pfp.Body)
	if err != nil {
		return nil, err
	}
	resizedAvatar := resize.Resize(RankAvatarDiameter, RankAvatarDiameter, avatar, resize.Lanczos3)

	avatarRadius := float64(RankAvatarDiameter) / 2
	avatarCenterX := float64(RankAvatarX) + avatarRadius
	avatarCenterY := float64(RankAvatarY) + avatarRadius

	dc.Push()
	dc.DrawCircle(avatarCenterX, avatarCenterY, avatarRadius)
	dc.Clip()
	dc.DrawImage(resizedAvatar, RankAvatarX, RankAvatarY)
	dc.Pop()
	// gg's Pop() intentionally does not restore the clip mask, so it must be
	// cleared explicitly or every draw after this point stays clipped to the
	// avatar circle.
	dc.ResetClip()

	// Text.
	dc.SetColor(color.White)

	fontSize := RankUsernameMaxSize
	if len(memberName) > 20 {
		fontSize, err = dynamicFontSize(dc, memberName, RankUsernameMaxSize, 0.6)
		if err != nil {
			return nil, err
		}
	}
	if err := dc.LoadFontFace(rankCardFontPath, float64(fontSize)); err != nil {
		return nil, err
	}
	dc.SetColor(color.White)
	dc.DrawStringAnchored(memberName, RankUsernameX, RankUsernameY, 0, 1)

	xpProgress := fmt.Sprintf("%s/%s", Humanize(lvlData.CurrentXp), Humanize(lvlData.XpForNextLvl))
	if err := dc.LoadFontFace(rankCardFontPath, 32); err != nil {
		return nil, err
	}
	dc.DrawStringAnchored(xpProgress, RankXpTextX, RankXpTextY, 1, 1)

	// RANK and LEVEL: both the label and the number of a block are
	// right-aligned to the *same* x-anchor, so they can never drift apart
	// from each other regardless of digit count. The two blocks are then
	// spaced apart using the actually-measured LEVEL block width.
	levelX := float64(RankBlockRightX)

	if err := dc.LoadFontFace(rankCardFontPath, RankLabelFontSize); err != nil {
		return nil, err
	}
	dc.DrawStringAnchored("LEVEL", levelX, RankLabelY, 1, 1)
	levelLabelW, _ := dc.MeasureString("LEVEL")

	levelStr := strconv.Itoa(lvlData.Lvl)
	if err := dc.LoadFontFace(rankCardFontPath, RankNumberFontSize); err != nil {
		return nil, err
	}
	dc.DrawStringAnchored(levelStr, levelX, RankNumberY, 1, 1)
	levelNumW, _ := dc.MeasureString(levelStr)

	levelBlockWidth := math.Max(levelLabelW, levelNumW)
	rankX := levelX - levelBlockWidth - RankBlockGap

	rankStr := fmt.Sprintf("#%d", user.Rank)
	if err := dc.LoadFontFace(rankCardFontPath, RankNumberFontSize); err != nil {
		return nil, err
	}
	dc.DrawStringAnchored(rankStr, rankX, RankNumberY, 1, 1)

	if err := dc.LoadFontFace(rankCardFontPath, RankLabelFontSize); err != nil {
		return nil, err
	}
	dc.DrawStringAnchored("RANK", rankX, RankLabelY, 1, 1)

	return dc.Image(), nil
}
