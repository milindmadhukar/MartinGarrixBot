package utils

import (
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
	"github.com/nfnt/resize"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

const rankCardFontPath = "assets/font.ttf"
const rankCardBackgroundsDir = "assets/backgrounds"

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

// averageColor returns the mean RGB colour of every pixel in img, used as
// the progress-bar fill colour so it always matches the chosen background.
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

func RankPicture(user db.GetUserLevelDataRow, memberName string, avatarUrl string) (image.Image, error) {
	lvlData := GetUserLevelData(user.TotalXp)
	percentage := float64(lvlData.CurrentXp) / float64(lvlData.XpForNextLvl)

	bgPath, err := pickRandomBackground()
	if err != nil {
		return nil, err
	}

	bgImg, err := loadImage(bgPath)
	if err != nil {
		return nil, err
	}
	background := coverResizeCrop(bgImg, RankCardWidth, RankCardHeight)
	fillColor := averageColor(background)

	ctx := gg.NewContext(RankCardWidth, RankCardHeight)
	ctx.DrawImage(background, 0, 0)

	// Translucent rounded panel, inset from the canvas edges.
	panelX := float64(RankPanelInset)
	panelY := float64(RankPanelInset)
	panelW := float64(RankCardWidth - 2*RankPanelInset)
	panelH := float64(RankCardHeight - 2*RankPanelInset)
	ctx.DrawRoundedRectangle(panelX, panelY, panelW, panelH, RankPanelRadius)
	ctx.SetRGBA255(18, 18, 18, 145)
	ctx.Fill()

	// Progress bar track (grey stadium/pill).
	barX := float64(RankBarX)
	barY := float64(RankBarY)
	barW := float64(RANK_PICTURE_WIDTH)
	barH := float64(RankBarHeight)
	ctx.DrawRoundedRectangle(barX, barY, barW, barH, barH/2)
	ctx.SetRGBA255(93, 93, 93, 255)
	ctx.Fill()

	// Progress fill, clipped to the same pill shape so only its left cap is
	// rounded (matching how much of the bar is actually filled).
	fillWidth := barW * percentage
	if fillWidth > 0 {
		ctx.Push()
		ctx.DrawRoundedRectangle(barX, barY, barW, barH, barH/2)
		ctx.Clip()
		ctx.SetColor(fillColor)
		ctx.DrawRectangle(barX, barY, fillWidth, barH)
		ctx.Fill()
		ctx.Pop()
		// See the matching comment above the avatar's ResetClip() call.
		ctx.ResetClip()
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

	ctx.Push()
	ctx.DrawCircle(avatarCenterX, avatarCenterY, avatarRadius)
	ctx.Clip()
	ctx.DrawImage(resizedAvatar, RankAvatarX, RankAvatarY)
	ctx.Pop()
	// gg's Pop() intentionally does not restore the clip mask, so it must be
	// cleared explicitly or every draw after this point stays clipped to the
	// avatar circle.
	ctx.ResetClip()

	// Text.
	ctx.SetColor(color.White)

	fontSize := RankUsernameMaxSize
	if len(memberName) > 20 {
		fontSize, err = dynamicFontSize(ctx, memberName, RankUsernameMaxSize, 0.6)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.LoadFontFace(rankCardFontPath, float64(fontSize)); err != nil {
		return nil, err
	}
	ctx.SetColor(color.White)
	ctx.DrawStringAnchored(memberName, RankUsernameX, RankUsernameY, 0, 1)

	xpProgress := fmt.Sprintf("%s/%s", Humanize(lvlData.CurrentXp), Humanize(lvlData.XpForNextLvl))
	if err := ctx.LoadFontFace(rankCardFontPath, 32); err != nil {
		return nil, err
	}
	ctx.DrawStringAnchored(xpProgress, RankXpTextX, RankXpTextY, 1, 1)

	// RANK and LEVEL: both the label and the number of a block are
	// right-aligned to the *same* x-anchor, so they can never drift apart
	// from each other regardless of digit count. The two blocks are then
	// spaced apart using the actually-measured LEVEL block width.
	levelX := float64(RankBlockRightX)

	if err := ctx.LoadFontFace(rankCardFontPath, RankLabelFontSize); err != nil {
		return nil, err
	}
	ctx.DrawStringAnchored("LEVEL", levelX, RankLabelY, 1, 1)
	levelLabelW, _ := ctx.MeasureString("LEVEL")

	levelStr := strconv.Itoa(lvlData.Lvl)
	if err := ctx.LoadFontFace(rankCardFontPath, RankNumberFontSize); err != nil {
		return nil, err
	}
	ctx.DrawStringAnchored(levelStr, levelX, RankNumberY, 1, 1)
	levelNumW, _ := ctx.MeasureString(levelStr)

	levelBlockWidth := math.Max(levelLabelW, levelNumW)
	rankX := levelX - levelBlockWidth - RankBlockGap

	rankStr := fmt.Sprintf("#%d", user.Rank)
	if err := ctx.LoadFontFace(rankCardFontPath, RankNumberFontSize); err != nil {
		return nil, err
	}
	ctx.DrawStringAnchored(rankStr, rankX, RankNumberY, 1, 1)

	if err := ctx.LoadFontFace(rankCardFontPath, RankLabelFontSize); err != nil {
		return nil, err
	}
	ctx.DrawStringAnchored("RANK", rankX, RankLabelY, 1, 1)

	return ctx.Image(), nil
}
