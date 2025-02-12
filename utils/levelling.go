package utils

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"

	"github.com/nfnt/resize"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

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

var (
	colors = map[string]color.RGBA{
		"red":    {255, 0, 0, 255},
		"green":  {0, 255, 0, 255},
		"yellow": {255, 255, 0, 255},
		"pink":   {255, 0, 255, 255},
	}
)

// Helper function to measure text width
func measureString(face font.Face, text string) fixed.Int26_6 {
	return font.MeasureString(face, text)
}

func RankPicture(user db.GetUserLevelDataRow, memberName string, avatarUrl string) (image.Image, error) {
	lvlData := GetUserLevelData(user.TotalXp.Int32)
	percentage := float64(lvlData.CurrentXp) / float64(lvlData.XpForNextLvl)

	bgImgFile, err := os.Open("assets/grey_bg.png")
	if err != nil {
		return nil, err
	}

	base, err := png.Decode(bgImgFile)
	if err != nil {
		return nil, err
	}

	// TODO: Add more colours / templates
	primaryColours := []string{"red", "green", "yellow", "pink"}
	primaryColourIdx := rand.Intn(len(primaryColours))
	primaryColour := primaryColours[primaryColourIdx]

	progressBar := image.NewRGBA(
		image.Rect(
			0,
			0,
			int(RANK_PICTURE_WIDTH*percentage),
			50,
		),
	)

	for x := 0; x < progressBar.Bounds().Dx(); x++ {
		for y := 0; y < progressBar.Bounds().Dy(); y++ {
			progressBar.Set(x, y, colors[primaryColour])
		}
	}

	draw.Draw(base.(draw.Image),
		image.Rect(261, 194, 261+progressBar.Bounds().Dx(), 194+progressBar.Bounds().Dy()),
		progressBar,
		image.Point{0, 0},
		draw.Over)

	templateFile, err := os.Open("assets/" + primaryColour + ".png")
	if err != nil {
		return nil, err
	}

	template, err := png.Decode(templateFile)
	if err != nil {
		return nil, err
	}

	draw.Draw(base.(draw.Image),
		template.Bounds(),
		template,
		image.Point{0, 0},
		draw.Over)

	pfp, err := http.Get(avatarUrl)
	if err != nil {
		return nil, err
	}

	avatar, err := png.Decode(pfp.Body)
	if err != nil {
		return nil, err
	}

	resizedAvatar := resize.Resize(173, 173, avatar, resize.Lanczos3)

	circleAvatar := image.NewRGBA(image.Rect(0, 0, 173, 173))

	// PERF: A very bad vay of masking
	for x := 0; x < 173; x++ {
		for y := 0; y < 173; y++ {
			dx := float64(x - 173/2)
			dy := float64(y - 173/2)
			d := math.Sqrt(dx*dx + dy*dy)

			if d <= float64(173/2) {
				circleAvatar.Set(x, y, resizedAvatar.At(x, y))
			} else {
				circleAvatar.Set(x, y, color.RGBA{0, 0, 0, 0})
			}
		}
	}

	draw.Draw(base.(draw.Image),
		image.Rect(43, 63, 43+circleAvatar.Bounds().Dx(), 63+circleAvatar.Bounds().Dy()),
		circleAvatar,
		image.Point{0, 0},
		draw.Over)

	fontFile, err := os.Open("assets/font.ttf")
	if err != nil {
		return nil, err
	}
	defer fontFile.Close()

	fontBytes, err := io.ReadAll(fontFile)
	if err != nil {
		return nil, err
	}

	textDrawer, err := NewTextDrawer(base.(draw.Image), fontBytes)
	if err != nil {
		return nil, err
	}

	// Member name with dynamic font size
	fontSize := 36
	if len(memberName) > 20 {
		fontSize = textDrawer.calculateDynamicFontSize(memberName, 36, 0.6)
	}
	if err := textDrawer.drawText(memberName, 284, 145, fontSize); err != nil {
		return nil, err
	}

	// XP Progress
	xpProgress := fmt.Sprintf("%s/%s",
		Humanize(lvlData.CurrentXp),
		Humanize(lvlData.XpForNextLvl))
	if err := textDrawer.drawTextRightAligned(xpProgress, 925, 150, 32); err != nil {
		return nil, err
	}

	levelX := 845
	face, err := textDrawer.createFace(22)
	if err != nil {
		return nil, err
	}
	levelLabelWidth := measureString(face, "LEVEL")
	rankLabelWidth := measureString(face, "RANK")

	if err := textDrawer.drawTextRightAligned("LEVEL", levelX, 77, 22); err != nil {
		return nil, err
	}
	if err := textDrawer.drawText(strconv.Itoa(lvlData.Lvl), levelX+10, 50, 50); err != nil {
		return nil, err
	}

	const SPACING_BETWEEN_RANK_AND_LEVEL = 100
	rankX := levelX - int(levelLabelWidth.Ceil()) - SPACING_BETWEEN_RANK_AND_LEVEL

	// Draw rank number and label
	rank := user.Rank
	rankText := fmt.Sprintf("#%d", rank)
	if err := textDrawer.drawTextRightAligned(rankText, rankX+rankLabelWidth.Ceil(), 50, 50); err != nil {
		return nil, err
	}
	if err := textDrawer.drawTextRightAligned("RANK", rankX, 77, 22); err != nil {
		return nil, err
	}

	return base, nil
}
