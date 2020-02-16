package main

import (
	"bytes"
	"image"
	_ "image/jpeg" // jpeg
	_ "image/png"  // png

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/text"
	"github.com/golang/freetype/truetype"
	"github.com/g4me92bd777b8b16ed4c/assets"
)

// IM is pixel
var IM = pixel.IM

// ZV is pixel
var ZV = pixel.ZV

// V = pixel
var V = pixel.V

// R = pixel
var R = pixel.R

func init() {

}

// func p2f(p pixel.Vec) [2]float64 {
// 	return [2]float64{
// 		p.X,
// 		p.Y,
// 	}
// }
// func i642V(x, y int64) pixel.Vec {
// 	return pixel.V(float64(x), float64(y))
// }

func mkfont(playerid uint64, fontsize float64, font string) (t *text.Text) {
	if fontsize == 0 {
		fontsize = PageAmount
	}
	if font == "" {
		font = "font/computer-font.ttf"
	}
	t = loadTTF(font, fontsize, pixel.ZV)
	t.Color = colors[playerid%uint64(len(colors))]
	return t
}

func mustLoadPicture(path string) pixel.Picture {
	file := assets.MustAsset(path)

	img, _, err := image.Decode(bytes.NewReader(file))
	if err != nil {
		panic(err)
	}
	return pixel.PictureDataFromImage(img)
}
func loadPicture(path string) (pixel.Picture, error) {
	file := assets.MustAsset(path)

	img, _, err := image.Decode(bytes.NewReader(file))
	if err != nil {
		return nil, err
	}
	return pixel.PictureDataFromImage(img), nil
}

// func loadPictureFromFile(path string) (pixel.Picture, error) {
// 	file, err := os.Open(path)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer file.Close()
// 	img, _, err := image.Decode(file)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return pixel.PictureDataFromImage(img), nil
// // }
// func loadSprite(path string) (*pixel.Sprite, error) {
// 	pic, err := loadPicture(path)
// 	if err != nil {
// 		panic(err)
// 	}
// 	sprite := pixel.NewSprite(pic, pic.Bounds())
// 	return sprite, nil
// }

func loadTTF(path string, size float64, origin pixel.Vec) *text.Text {
	b := assets.MustAsset(path)

	font, err := truetype.Parse(b)
	if err != nil {
		panic(err)
	}
	face := truetype.NewFace(font, &truetype.Options{
		Size:              size,
		GlyphCacheEntries: 1,
	})
	atlas := text.NewAtlas(face, text.ASCII)
	txt := text.New(origin, atlas)
	return txt

}
