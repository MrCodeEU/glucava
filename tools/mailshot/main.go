// Command mailshot renders sample notification emails (made-up data) to PNG
// files, for the README and the project site. Development tool:
//
//	CHROME_PATH=/usr/bin/chromium go run ./tools/mailshot -out docs/img
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/pocketbase/pocketbase/tools/mailer"

	"github.com/MrCodeEU/glucava/internal/digest"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// htmlOf renders m through the real Email channel and returns the HTML part,
// with the inline chart turned into a data URI so it displays standalone.
func htmlOf(m notify.Message) string {
	var out string
	e := &notify.Email{
		To: "you@example.com", From: mail.Address{Address: "glucava@example.com"},
		SendFunc: func(msg *mailer.Message) error {
			out = msg.HTML
			if r, ok := msg.InlineAttachments["chart.png"]; ok {
				b, _ := io.ReadAll(r)
				out = strings.Replace(out, "cid:chart.png", "data:image/png;base64,"+base64.StdEncoding.EncodeToString(b), 1)
			}
			return nil
		},
	}
	m.Link, m.LinkLabel = notify.LinkFor("https://glucava.example.com", m)
	if err := e.Send(context.Background(), m); err != nil {
		log.Fatal(err)
	}
	return out
}

func main() {
	outDir := flag.String("out", "docs/img", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	loc := time.UTC
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, loc)
	mk := func(id, name string, days int, tir, min, below float64) jobs.Activity {
		return jobs.Activity{StravaID: id, Name: name, Sport: "Run", Start: now.AddDate(0, 0, -days), Duration: 52 * time.Minute, Status: jobs.StatusDone,
			Summary: &stats.Summary{Count: 11, TIR: tir, Min: min, Max: 176, Avg: 118, Below: below, Above: 100 - tir - below, Start: 118, End: 96}}
	}
	a := mk("140100", "Morning Run", 1, 94, 71, 0)
	var samples []stats.Sample
	for i := -6; i < 16; i++ {
		samples = append(samples, stats.Sample{Time: a.Start.Add(time.Duration(i) * 5 * time.Minute), Value: 128 + 40*math.Sin(float64(i)/5) - float64(i)*1.5})
	}
	activity, _ := digest.ActivityMessage(a, render.MgDL, stats.DefaultRange, samples, loc)
	cur := []jobs.Activity{mk("1", "Easy Run", 6, 92, 84, 0), mk("2", "Intervals", 4, 71, 58, 6), mk("3", "Long Run", 2, 85, 74, 0), mk("4", "Recovery Jog", 1, 100, 90, 0)}
	weekly, _ := digest.WeeklyMessage(cur, []jobs.Activity{mk("5", "Tempo", 9, 88, 80, 0)}, now.AddDate(0, 0, -7), now, render.MgDL, loc)
	alert := notify.Message{Type: "session_expired", Severity: "error", Title: notify.Title("session_expired"),
		Body: "polling failed: the Strava session has expired, import fresh cookies"}
	for _, m := range []*notify.Message{&activity, &weekly, &alert} {
		m.Time = now
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(os.Getenv("CHROME_PATH")), chromedp.Flag("no-sandbox", true), chromedp.WindowSize(640, 1000))
	actx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(actx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, time.Minute)
	defer cancel()

	for name, m := range map[string]notify.Message{"mail-activity": activity, "mail-weekly": weekly, "mail-alert": alert} {
		doc := htmlOf(m)
		var png []byte
		err := chromedp.Run(ctx,
			chromedp.Navigate("about:blank"),
			chromedp.ActionFunc(func(ctx context.Context) error {
				tree, err := page.GetFrameTree().Do(ctx)
				if err != nil {
					return err
				}
				return page.SetDocumentContent(tree.Frame.ID, doc).Do(ctx)
			}),
			chromedp.Sleep(300*time.Millisecond),
			chromedp.Screenshot("body > div", &png, chromedp.ByQuery),
		)
		if err != nil {
			log.Fatal(err)
		}
		f := filepath.Join(*outDir, name+".png")
		if err := os.WriteFile(f, png, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Println(f)
	}
}
