// Command shot logs in to a running glucava and saves full-page screenshots.
// Development tool: go run ./tools/shot -url http://127.0.0.1:8090 -out /tmp/shots
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// defaultPaths covers every page: dashboard, two activities, every Overview
// range (presets, compare, a custom range and a rejected one), and the
// settings/ops pages. {today-N} in a path is the date N days ago.
const defaultPaths = "/,/activity/140100,/activity/140098,/stats?range=7d,/stats?range=14d,/stats?range=30d,/stats?range=90d,/stats?range=all," +
	"/stats?range=30d&compare=prev,/stats?from={today-20}&to={today-6},/stats?from=2001-01-01&to=2000-01-01," +
	"/strava,/settings,/tokens,/events,/logs"

var (
	todayRe = regexp.MustCompile(`\{today-(\d+)\}`)
	nonName = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

// problems collects browser console errors, uncaught exceptions and CSP
// violations, so a page that looks fine but is broken still fails the run.
type problems struct {
	mu     sync.Mutex
	where  string
	ignore *regexp.Regexp
	list   []string
}

func (p *problems) setWhere(w string) {
	p.mu.Lock()
	p.where = w
	p.mu.Unlock()
}

func (p *problems) add(kind, msg string) {
	if p.ignore != nil && p.ignore.MatchString(msg) {
		return
	}
	p.mu.Lock()
	p.list = append(p.list, fmt.Sprintf("%s: %s: %s", p.where, kind, strings.TrimSpace(msg)))
	p.mu.Unlock()
}

func (p *problems) listen(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			if e.Type != runtime.APITypeError {
				return
			}
			var parts []string
			for _, a := range e.Args {
				parts = append(parts, strings.Trim(string(a.Value), `"`)+a.Description)
			}
			p.add("console.error", strings.Join(parts, " "))
		case *runtime.EventExceptionThrown:
			msg := e.ExceptionDetails.Text
			if e.ExceptionDetails.Exception != nil {
				msg += " " + e.ExceptionDetails.Exception.Description
			}
			p.add("exception", msg)
		case *log.EventEntryAdded:
			// Chrome reports CSP violations and failed subresource loads here.
			if e.Entry.Level == log.LevelError {
				p.add("browser log ("+string(e.Entry.Source)+")", e.Entry.Text+" "+e.Entry.URL)
			}
		}
	})
}

func main() {
	base := flag.String("url", "http://127.0.0.1:8090", "server URL")
	out := flag.String("out", "shots", "output directory")
	email := flag.String("email", "demo@example.test", "login email")
	pass := flag.String("password", "demo-password-123", "login password")
	mode := flag.String("mode", "light", "light or dark")
	width := flag.Int("width", 1280, "viewport width")
	flow := flag.Bool("flow", false, "click through the main actions instead of visiting pages")
	matrix := flag.Bool("matrix", false, "every page at 1280 and 390 px, light and dark (ignores -mode and -width)")
	ignore := flag.String("ignore", "", "regexp of console messages to tolerate, e.g. a script that is not deployed yet")
	lang := flag.String("lang", "", "browser language, e.g. de (glucava follows it while Settings > Language is automatic)")
	paths := flag.String("paths", defaultPaths, "comma-separated paths")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		slog.Error("mkdir out dir", "err", err)
		os.Exit(1)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(os.Getenv("CHROME_PATH")), chromedp.Flag("no-sandbox", true), chromedp.WindowSize(*width, 900))
	if *lang != "" {
		opts = append(opts, chromedp.Flag("lang", *lang))
	}
	actx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(actx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()

	probs := &problems{}
	if *ignore != "" {
		re, err := regexp.Compile(*ignore)
		if err != nil {
			slog.Error("bad -ignore", "err", err)
			os.Exit(2)
		}
		probs.ignore = re
	}
	probs.listen(ctx)
	// finish reports collected browser problems; any of them is a failure.
	finish := func() {
		if len(probs.list) == 0 {
			return
		}
		fmt.Fprintf(os.Stderr, "\n%d browser problem(s):\n", len(probs.list))
		for _, l := range probs.list {
			fmt.Fprintln(os.Stderr, "  "+l)
		}
		os.Exit(1)
	}

	curMode, curWidth := *mode, *width
	suffix := func() string {
		if *matrix {
			return fmt.Sprintf("-%d-%s", curWidth, curMode)
		}
		return "-" + curMode
	}
	var buf []byte
	shot := func(name string) {
		// Grow the viewport to the page first, so fixed elements (the phone tab
		// bar, toasts) sit at the bottom of the shot, as they would when scrolled there.
		var h int64
		if err := chromedp.Run(ctx, chromedp.Evaluate(`Math.ceil(document.documentElement.scrollHeight)`, &h)); err == nil && h > 0 {
			_ = chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(int64(curWidth), h, 1, curWidth < 600))
			defer func() {
				_ = chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(int64(curWidth), 900, 1, curWidth < 600))
			}()
		}
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
			slog.Error("screenshot", "err", err)
			os.Exit(1)
		}
		f := filepath.Join(*out, name+suffix()+".png")
		if err := os.WriteFile(f, buf, 0o644); err != nil {
			slog.Error("write screenshot", "err", err)
			os.Exit(1)
		}
		fmt.Println(f)
	}
	setMode := chromedp.ActionFunc(func(c context.Context) error {
		return chromedp.Evaluate(fmt.Sprintf(`document.documentElement.dataset.mode=%q`, curMode), nil).Do(c)
	})
	setViewport := func(w int) error {
		curWidth = w
		return chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(int64(w), 900, 1, w < 600))
	}

	if err := chromedp.Run(ctx, chromedp.Navigate(*base+"/login"), setMode, chromedp.Sleep(400*time.Millisecond)); err != nil {
		slog.Error("navigate to login", "err", err)
		os.Exit(1)
	}
	if *matrix {
		for _, w := range []int{1280, 390} {
			_ = setViewport(w)
			for _, m := range []string{"light", "dark"} {
				curMode = m
				probs.setWhere(fmt.Sprintf("/login (%dpx, %s)", w, m))
				if err := chromedp.Run(ctx, chromedp.Navigate(*base+"/login"), setMode, chromedp.Sleep(300*time.Millisecond)); err != nil {
					slog.Error("navigate to login", "err", err)
					os.Exit(1)
				}
				shot("login")
			}
		}
		_ = setViewport(1280)
		curMode = "light"
	} else {
		probs.setWhere("/login")
		shot("login")
	}
	probs.setWhere("login form")
	err := chromedp.Run(ctx,
		chromedp.SendKeys("#email", *email), chromedp.SendKeys("#password", *pass),
		chromedp.Click(`form[action="/login"] button[type="submit"]`),
		chromedp.WaitVisible("main"),
	)
	if err != nil {
		slog.Error("login", "err", err)
		os.Exit(1)
	}
	if *flow {
		runFlow(ctx, *base, shot, setMode)
		finish()
		return
	}

	visit := func(p string) {
		name := strings.Trim(nonName.ReplaceAllString(strings.NewReplacer("?range=", "-", "&compare=prev", "-compare", "?from=", "-custom-").Replace(todayRe.ReplaceAllString(p, "d$1")), "-"), "-")
		if name == "" {
			name = "dashboard"
		}
		p = todayRe.ReplaceAllStringFunc(p, func(m string) string {
			n, _ := strconv.Atoi(todayRe.FindStringSubmatch(m)[1])
			return time.Now().AddDate(0, 0, -n).Format("2006-01-02")
		})
		probs.setWhere(fmt.Sprintf("%s (%dpx, %s)", p, curWidth, curMode))
		if err := chromedp.Run(ctx, chromedp.Navigate(*base+p), setMode, chromedp.Sleep(900*time.Millisecond)); err != nil {
			slog.Error("navigate", "path", p, "err", err)
			os.Exit(1)
		}
		shot(name)
	}
	list := strings.Split(*paths, ",")
	if !*matrix {
		for _, p := range list {
			visit(p)
		}
		finish()
		return
	}
	for _, w := range []int{1280, 390} {
		if err := setViewport(w); err != nil {
			slog.Error("viewport", "err", err)
			os.Exit(1)
		}
		for _, m := range []string{"light", "dark"} {
			curMode = m
			for _, p := range list {
				visit(p)
			}
		}
	}
	finish()
}

func runFlow(ctx context.Context, base string, shot func(string), setMode chromedp.Action) {
	must := func(err error) {
		if err != nil {
			slog.Error("flow step failed", "err", err)
			os.Exit(1)
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

	// 2b. A live patch must not rebuild the activity chart: the same element
	// stays in the page, keeps its rendered SVG, and keeps the zoom.
	var kept bool
	must(chromedp.Run(ctx, chromedp.Navigate(base+"/activity/140100"), setMode, chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const el = document.querySelector('#activity-chart');
			if (!el) return false;
			window.__chartEl = el;
			const ch = el._chart || null;
			window.__zoomBefore = ch ? JSON.stringify(ch.getOption().dataZoom.map(z => [z.start, z.end])) : '';
			return true;
		})()`, &kept),
		chromedp.Click(`button[data-on\:click*="/actions/reprocess"]`, chromedp.ByQuery), chromedp.Sleep(6*time.Second)))
	var same bool
	must(chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const el = document.querySelector('#activity-chart');
		return !!el && el === window.__chartEl && !!el.shadowRoot && !!el.shadowRoot.querySelector('svg');
	})()`, &same)))
	shot("flow2b-activity-chart-after-patch")
	fmt.Println("activity chart present before patch:", kept, "· same element with rendered SVG after patch:", same)

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

	// 6. Overview layout: hide a card and move another down in the settings,
	// save, reload, and read both back (this exercises the nested signals
	// overviewOn.<id> and the reorder buttons for real).
	var kpisOn, order string
	must(chromedp.Run(ctx, chromedp.Navigate(base+"/settings"), setMode, chromedp.Sleep(800*time.Millisecond),
		chromedp.Click(`//button[contains(., "Description and chart")]`, chromedp.BySearch), chromedp.Sleep(300*time.Millisecond),
		chromedp.Click(`#overviewOn_kpis`, chromedp.ByQuery),
		chromedp.Click(`#overview-move-tir-down`, chromedp.ByQuery),
		chromedp.SetValue("#overviewOpt_heatmap_metric", "tir", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('#overviewOpt_heatmap_metric').dispatchEvent(new Event('change',{bubbles:true}))`, nil),
		chromedp.Click(`button[data-on\:click*="/actions/settings"]`, chromedp.ByQuery), chromedp.Sleep(1500*time.Millisecond)))
	shot("flow6-overview-layout-saved")
	must(chromedp.Run(ctx, chromedp.Navigate(base+"/settings"), chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelector('#overviewOn_kpis').checked)`, &kpisOn),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('[id^="overviewOn_"]')).sort((a,b)=>a.parentElement.style.order-b.parentElement.style.order).map(e=>e.id.slice(10)).join(',')`, &order)))
	fmt.Println("kpis card enabled after reload (want false):", kpisOn)
	fmt.Println("card order after reload (want agp before tir):", order)
}
