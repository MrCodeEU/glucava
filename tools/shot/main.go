// Command shot logs in to a running glucava and saves full-page screenshots.
// Development tool: go run ./tools/shot -url http://127.0.0.1:8090 -out /tmp/shots
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

func main() {
	base := flag.String("url", "http://127.0.0.1:8090", "server URL")
	out := flag.String("out", "shots", "output directory")
	email := flag.String("email", "demo@example.test", "login email")
	pass := flag.String("password", "demo-password-123", "login password")
	mode := flag.String("mode", "light", "light or dark")
	width := flag.Int("width", 1280, "viewport width")
	flow := flag.Bool("flow", false, "click through the main actions instead of visiting pages")
	paths := flag.String("paths", "/,/activity/140100,/activity/140098,/strava,/settings,/tokens,/events", "comma-separated paths")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(os.Getenv("CHROME_PATH")), chromedp.Flag("no-sandbox", true), chromedp.WindowSize(*width, 900))
	actx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(actx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var buf []byte
	shot := func(name string) {
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
			log.Fatal(err)
		}
		f := filepath.Join(*out, name+"-"+*mode+".png")
		if err := os.WriteFile(f, buf, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Println(f)
	}
	setMode := chromedp.Evaluate(fmt.Sprintf(`document.documentElement.dataset.mode=%q`, *mode), nil)

	if err := chromedp.Run(ctx, chromedp.Navigate(*base+"/login"), setMode, chromedp.Sleep(400*time.Millisecond)); err != nil {
		log.Fatal(err)
	}
	shot("login")
	err := chromedp.Run(ctx,
		chromedp.SendKeys("#email", *email), chromedp.SendKeys("#password", *pass),
		chromedp.Click(`form[action="/login"] button[type="submit"]`),
		chromedp.WaitVisible("main"),
	)
	if err != nil {
		log.Fatal(err)
	}
	if *flow {
		runFlow(ctx, *base, shot, setMode)
		return
	}
	for _, p := range strings.Split(*paths, ",") {
		name := strings.Trim(strings.ReplaceAll(p, "/", "-"), "-")
		if name == "" {
			name = "dashboard"
		}
		if err := chromedp.Run(ctx, chromedp.Navigate(*base+p), setMode, chromedp.Sleep(900*time.Millisecond)); err != nil {
			log.Fatal(err)
		}
		shot(name)
	}
}

func runFlow(ctx context.Context, base string, shot func(string), setMode chromedp.Action) {
	must := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	text := func(sel string) string {
		var s string
		must(chromedp.Run(ctx, chromedp.Text(sel, &s, chromedp.ByQuery)))
		return s
	}

	// 1. "Check Strava now" should make a new activity appear live, without a reload.
	must(chromedp.Run(ctx, chromedp.Navigate(base+"/"), setMode, chromedp.Sleep(800*time.Millisecond),
		chromedp.Click(`button[data-on\:click*="/actions/poll"]`, chromedp.ByQuery), chromedp.Sleep(7*time.Second)))
	shot("flow1-dashboard-after-poll")
	fmt.Println("dashboard mentions new run:", strings.Contains(text("#live"), "Lunch Run"))

	// 2. Reprocess the failed activity.
	must(chromedp.Run(ctx, chromedp.Navigate(base+"/activity/140098"), setMode, chromedp.Sleep(800*time.Millisecond),
		chromedp.Click(`button[data-on\:click*="/actions/reprocess"]`, chromedp.ByQuery), chromedp.Sleep(5*time.Second)))
	shot("flow2-reprocess")
	fmt.Println("failed activity now done:", strings.Contains(text("#activity-body"), "Done"))

	// 3. Create a token.
	must(chromedp.Run(ctx, chromedp.Navigate(base+"/tokens"), setMode, chromedp.Sleep(800*time.Millisecond),
		chromedp.SendKeys("#tokenName", "phone"),
		chromedp.Click(`button[data-on\:click*="/actions/tokens/create"]`, chromedp.ByQuery), chromedp.Sleep(1500*time.Millisecond)))
	shot("flow3-token")
	fmt.Println("token shown:", strings.Contains(text("#token-reveal"), "gst_"))

	// 4. Save settings with a changed value, then reload and read it back.
	var low string
	must(chromedp.Run(ctx, chromedp.Navigate(base+"/settings"), setMode, chromedp.Sleep(800*time.Millisecond),
		chromedp.SetValue("#rangeLow", "75", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('#rangeLow').dispatchEvent(new Event('input',{bubbles:true}))`, nil),
		chromedp.Click(`button[data-on\:click*="/actions/settings"]`, chromedp.ByQuery), chromedp.Sleep(1500*time.Millisecond)))
	shot("flow4-settings-saved")
	must(chromedp.Run(ctx, chromedp.Navigate(base+"/settings"), chromedp.Sleep(800*time.Millisecond),
		chromedp.Value("#rangeLow", &low, chromedp.ByQuery)))
	fmt.Println("range low after reload:", low)

	// 5. Test the Strava session.
	must(chromedp.Run(ctx, chromedp.Navigate(base+"/strava"), setMode, chromedp.Sleep(800*time.Millisecond),
		chromedp.Click(`button[data-on\:click*="/actions/strava/test"]`, chromedp.ByQuery), chromedp.Sleep(2500*time.Millisecond)))
	shot("flow5-session-test")
	fmt.Println("session shows Valid:", strings.Contains(text("#strava-status"), "Valid"))
}
