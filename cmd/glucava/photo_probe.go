package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/MrCodeEU/glucava/internal/strava"
)

// runPhotoProbe backs "glucava strava check <id> --photo".
func runPhotoProbe(ctx context.Context, w *strava.Writer, id, shot string) error {
	img := image.NewRGBA(image.Rect(0, 0, 240, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 240; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y * 2), 160, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	p, err := w.ProbePhoto(ctx, id, buf.Bytes())
	if err != nil {
		return err
	}
	fmt.Printf("file input matched: %s\nmedia signal:       %d -> %d\nrequests (non-GET): %s\nfinal page state:   %s\n", p.Selector, p.Before, p.After, p.Requests, p.State)
	if shot != "" && len(p.Screenshot) > 0 {
		if err := os.WriteFile(shot, p.Screenshot, 0o600); err != nil {
			return err
		}
		fmt.Printf("screenshot written to %s (shows your activity page)\n", shot)
	}
	fmt.Println("nothing was saved.")
	return nil
}
