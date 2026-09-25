package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"

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
	tries, err := w.ProbePhoto(ctx, id, buf.Bytes())
	if err != nil {
		return err
	}
	for _, p := range tries {
		fmt.Printf("== %s\nuploads (no analytics):        %s\npreview images from the file:   %d\nopen dialogs:                   %s\npage state:                     %s\n", p.Method, p.Requests, p.Blobs, p.Dialogs, p.State)
		if shot != "" && len(p.Screenshot) > 0 {
			f := strings.TrimSuffix(shot, ".png") + "-" + p.Method + ".png"
			if err := os.WriteFile(f, p.Screenshot, 0o600); err != nil {
				return err
			}
			fmt.Printf("screenshot: %s (shows your activity page)\n", f)
		}
	}
	fmt.Println("nothing was saved.")
	return nil
}
