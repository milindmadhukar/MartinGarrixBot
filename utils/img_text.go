package utils

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type TextDrawer struct {
    drawer *font.Drawer
    font   *opentype.Font
}

func NewTextDrawer(dst draw.Image, fontBytes []byte) (*TextDrawer, error) {
    myFont, err := opentype.Parse(fontBytes)
    if err != nil {
        return nil, err
    }

    drawer := &font.Drawer{
        Dst: dst,
        Src: image.NewUniform(color.White),
    }

    return &TextDrawer{
        drawer: drawer,
        font:   myFont,
    }, nil
}

func (td *TextDrawer) createFace(fontSize int) (font.Face, error) {
    return opentype.NewFace(td.font, &opentype.FaceOptions{
        Size:    float64(fontSize),
        DPI:     72,
        Hinting: font.HintingFull,
    })
}

func (td *TextDrawer) drawText(text string, x, y, fontSize int) error {
    face, err := td.createFace(fontSize)
    if err != nil {
        return err
    }
    td.drawer.Face = face
    td.drawer.Dot = fixed.Point26_6{
        X: fixed.I(x),
        Y: fixed.I(y + fontSize),
    }
    td.drawer.DrawString(text)
    return nil
}

func (td *TextDrawer) drawTextRightAligned(text string, x, y, fontSize int) error {
    face, err := td.createFace(fontSize)
    if err != nil {
        return err
    }
    td.drawer.Face = face
    textWidth := measureString(face, text)
    td.drawer.Dot = fixed.Point26_6{
        X: fixed.I(x) - textWidth,
        Y: fixed.I(y + fontSize),
    }
    td.drawer.DrawString(text)
    return nil
}

func (td *TextDrawer) calculateDynamicFontSize(text string, maxSize int, imgFraction float64) int {
    fontSize := 12
    for {
        face, _ := td.createFace(fontSize)
        textWidth := measureString(face, text)
        if float64(textWidth.Ceil()) >= float64(RANK_PICTURE_WIDTH)*imgFraction {
            break
        }
        fontSize++
        if fontSize >= maxSize {
            break
        }
    }
    return fontSize - 1
}