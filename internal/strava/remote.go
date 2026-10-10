package strava

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Viewport of the remote browser. The client scales its pointer coordinates
// to this size, so it is also the size of every streamed frame.
const (
	RemoteWidth  = 1280
	RemoteHeight = 800
)

// remoteLockWait is how long StartRemote waits for a running poll or write.
var remoteLockWait = 30 * time.Second

// ErrRemoteBusy is returned when a remote browser is already open.
var ErrRemoteBusy = errors.New("strava: a remote browser session is already open")

// RemoteInput is one input event from the user's browser. Type is one of
// "move", "down", "up", "wheel", "keydown", "keyup", "text".
type RemoteInput struct {
	Type      string  `json:"type"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Button    string  `json:"button"` // "left", "middle", "right"
	DeltaX    float64 `json:"dx"`
	DeltaY    float64 `json:"dy"`
	Key       string  `json:"key"`
	Code      string  `json:"code"`
	Text      string  `json:"text"`
	Modifiers int64   `json:"mod"` // CDP bitmask: 1 alt, 2 ctrl, 4 meta, 8 shift
}

// RemoteSession is a live Chrome the user drives from the Glucava UI to sign
// in to Strava by hand. Nothing typed into it is stored or logged; only the
// resulting cookies are kept, through the same path as a cookie import.
//
// Frames are delivered latest-wins: a slow client skips frames instead of
// making Chrome queue them.
type RemoteSession struct {
	w      *Writer
	ctx    context.Context
	cancel context.CancelFunc
	close  func()

	mainFrame cdp.FrameID // the top-level frame; set before the first navigation
	frames    chan []byte
	done      chan struct{}
	once      sync.Once

	mu      sync.Mutex
	cookies []Cookie
	err     error
}

// StartRemote opens a remote browser on Strava's login page. It lives for at
// most ttl or until Close. Only one session exists at a time, and it holds the
// Writer's lock so no description write runs while the user is signing in.
func (w *Writer) StartRemote(ctx context.Context, ttl time.Duration) (*RemoteSession, error) {
	// A poll or write holds the lock for the length of its run; wait a
	// little for it instead of failing the click.
	for deadline := time.Now().Add(remoteLockWait); !w.mu.TryLock(); {
		if time.Now().After(deadline) {
			return nil, ErrRemoteBusy
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	// The session outlives the request that opened it, so it takes ctx's
	// values only, not its cancellation.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ttl)
	unlock := func() { w.mu.Unlock() }

	bctx, closeBrowser, err := w.launch(rctx, remoteOptions())
	if err != nil {
		cancel()
		unlock()
		return nil, err
	}
	s := &RemoteSession{
		w: w, ctx: bctx, cancel: cancel,
		frames: make(chan []byte, 1),
		done:   make(chan struct{}),
	}
	s.close = func() {
		s.once.Do(func() {
			cancel()
			closeBrowser()
			close(s.done)
			unlock()
		})
	}

	if err := s.setup(); err != nil {
		s.close()
		return nil, err
	}
	go s.watch()
	return s, nil
}

// remoteOptions makes the browser look like an ordinary desktop Chrome
// rather than an automation harness: no automation infobar flag and no
// AutomationControlled blink feature. The user signs in by hand with their
// own account; this only avoids needless bot challenges.
func remoteOptions() []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.WindowSize(RemoteWidth, RemoteHeight),
	}
}

// ssoSites are the sign-in providers Strava offers, plus the captcha hosts
// they embed. Page loads anywhere else are refused, so the remote browser
// cannot be used to browse the web from the server.
var ssoSites = []string{
	"google.com", "gstatic.com", "recaptcha.net", "googleusercontent.com",
	"apple.com", "facebook.com", "fbcdn.net",
}

// googleSite matches Google's sign-in hosts on any of its country domains
// (accounts.google.at, accounts.google.co.uk, accounts.google.com.br, ...):
// Google redirects to the one for the user's country.
var googleSite = regexp.MustCompile(`^([a-z0-9-]+\.)*google\.(com|[a-z]{2}|(co|com)\.[a-z]{2})$`)

// allowedPage reports whether the remote browser may load a page from host.
func (s *RemoteSession) allowedPage(host string) bool {
	base, err := url.Parse(s.w.cfg.BaseURL)
	if err == nil && sameSite(host, base.Hostname()) {
		return true
	}
	if googleSite.MatchString(host) {
		return true
	}
	for _, site := range ssoSites {
		if host == site || strings.HasSuffix(host, "."+site) {
			return true
		}
	}
	return false
}

func (s *RemoteSession) setup() error {
	chromedp.ListenTarget(s.ctx, func(ev any) {
		if rp, ok := ev.(*fetch.EventRequestPaused); ok {
			go s.gate(rp)
			return
		}
		f, ok := ev.(*page.EventScreencastFrame)
		if !ok {
			return
		}
		data := []byte(f.Data) // base64 text, passed to the client as is
		// Ack off the event goroutine: chromedp blocks further events
		// while a listener runs.
		go func() {
			_ = chromedp.Run(s.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				return page.ScreencastFrameAck(f.SessionID).Do(ctx)
			}))
		}()
		select { // latest wins
		case <-s.frames:
		default:
		}
		select {
		case s.frames <- data:
		default:
		}
	})
	err := chromedp.Run(s.ctx,
		s.maskAutomation(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			tree, err := page.GetFrameTree().Do(ctx)
			if err == nil {
				s.mainFrame = tree.Frame.ID
			}
			return err
		}),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{ResourceType: network.ResourceTypeDocument}}),
		emulation.SetDeviceMetricsOverride(RemoteWidth, RemoteHeight, 1, false),
		chromedp.Navigate(s.w.cfg.BaseURL+"/login"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return page.StartScreencast().
				WithFormat(page.ScreencastFormatJpeg).WithQuality(60).
				WithMaxWidth(RemoteWidth).WithMaxHeight(RemoteHeight).
				WithEveryNthFrame(1).Do(ctx)
		}),
	)
	if err != nil {
		return fmt.Errorf("strava: open remote browser: %w", err)
	}
	return nil
}

// frameSites are hosts that sign-in pages may embed as frames on top of the
// page allowlist: Google's cookie-sync frames and the usual captcha providers.
var frameSites = []string{"youtube.com", "hcaptcha.com", "cloudflare.com", "arkoselabs.com"}

// allowedFrame reports whether a sub-frame may load from host.
func (s *RemoteSession) allowedFrame(host string) bool {
	if s.allowedPage(host) {
		return true
	}
	for _, site := range frameSites {
		if host == site || strings.HasSuffix(host, "."+site) {
			return true
		}
	}
	return false
}

// privateHost reports whether host is a loopback, private or link-local
// address or a local name. The server's own network is never reachable
// from the remote browser, except the configured Strava base (tests).
func (s *RemoteSession) privateHost(host string) bool {
	if base, err := url.Parse(s.w.cfg.BaseURL); err == nil && host == base.Hostname() {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
}

// gate lets a page load through or refuses it: top-level navigations must be
// on the page allowlist, frames on the page or frame allowlist, and nothing
// may reach a private address. It fails closed if the main frame is unknown.
// Only the URL host is looked at; nothing from the request is logged.
func (s *RemoteSession) gate(rp *fetch.EventRequestPaused) {
	u, err := url.Parse(rp.Request.URL)
	allow := err == nil && s.mainFrame != ""
	if allow && u.Scheme != "about" && u.Scheme != "data" {
		host := u.Hostname()
		switch {
		case s.privateHost(host):
			allow = false
		case rp.FrameID == s.mainFrame:
			allow = s.allowedPage(host)
		default:
			allow = s.allowedFrame(host)
		}
	}
	_ = chromedp.Run(s.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if allow {
			return fetch.ContinueRequest(rp.RequestID).Do(ctx)
		}
		return fetch.FailRequest(rp.RequestID, network.ErrorReasonBlockedByClient).Do(ctx)
	}))
}

// Frames yields base64-encoded JPEG frames, latest only.
func (s *RemoteSession) Frames() <-chan []byte { return s.frames }

// Done is closed when the session has ended: signed in, timed out or closed.
func (s *RemoteSession) Done() <-chan struct{} { return s.done }

// Result returns the cookies captured at sign-in, or the reason the session
// ended without any. Valid after Done is closed.
func (s *RemoteSession) Result() ([]Cookie, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cookies, s.err
}

// Close ends the session and deletes the browser profile.
func (s *RemoteSession) Close() { s.close() }

// watch polls for a completed sign-in: off the login pages, on Strava, with
// the session cookie set. It then keeps the Strava cookies and closes.
func (s *RemoteSession) watch() {
	defer s.close()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			s.finish(nil, errors.New("strava: the sign-in browser timed out or was closed"))
			return
		case <-t.C:
		}
		cookies, ok := s.signedIn()
		if ok {
			s.finish(cookies, nil)
			return
		}
	}
}

func (s *RemoteSession) finish(c []Cookie, err error) {
	s.mu.Lock()
	s.cookies, s.err = c, err
	s.mu.Unlock()
}

func (s *RemoteSession) signedIn() ([]Cookie, bool) {
	var loc string
	if err := chromedp.Run(s.ctx, chromedp.Location(&loc)); err != nil {
		return nil, false
	}
	u, err := url.Parse(loc)
	base, berr := url.Parse(s.w.cfg.BaseURL)
	if err != nil || berr != nil || !sameSite(u.Hostname(), base.Hostname()) || isLoginURL(loc) {
		return nil, false
	}
	var out []Cookie
	err = chromedp.Run(s.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		got, err := network.GetCookies().WithURLs([]string{s.w.cfg.BaseURL}).Do(ctx)
		if err != nil {
			return err
		}
		for _, c := range got {
			ck := Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Secure: c.Secure, HTTPOnly: c.HTTPOnly}
			if c.Expires > 0 {
				ck.Expires = time.Unix(int64(c.Expires), 0).UTC()
			}
			out = append(out, ck)
		}
		return nil
	}))
	if err != nil || !HasSession(out) {
		return nil, false
	}
	return out, true
}

// sameSite reports whether host is the base host (with or without www) or one
// of its subdomains.
func sameSite(host, base string) bool {
	base = strings.TrimPrefix(base, "www.")
	return host == base || host == "www."+base || strings.HasSuffix(host, "."+base)
}

// Dispatch forwards one user input event to the page.
func (s *RemoteSession) Dispatch(ev RemoteInput) error {
	select {
	case <-s.done:
		return errors.New("strava: remote browser closed")
	default:
	}
	return chromedp.Run(s.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		switch ev.Type {
		case "move":
			return input.DispatchMouseEvent(input.MouseMoved, ev.X, ev.Y).WithModifiers(input.Modifier(ev.Modifiers)).Do(ctx)
		case "down", "up":
			t := input.MousePressed
			if ev.Type == "up" {
				t = input.MouseReleased
			}
			return input.DispatchMouseEvent(t, ev.X, ev.Y).
				WithButton(mouseButton(ev.Button)).WithClickCount(1).
				WithModifiers(input.Modifier(ev.Modifiers)).Do(ctx)
		case "wheel":
			return input.DispatchMouseEvent(input.MouseWheel, ev.X, ev.Y).
				WithDeltaX(ev.DeltaX).WithDeltaY(ev.DeltaY).Do(ctx)
		case "keydown", "keyup":
			t := input.KeyDown
			if ev.Type == "keyup" {
				t = input.KeyUp
			}
			p := input.DispatchKeyEvent(t).WithKey(ev.Key).WithCode(ev.Code).
				WithModifiers(input.Modifier(ev.Modifiers))
			if t == input.KeyDown && len([]rune(ev.Key)) == 1 {
				// A printable key also needs its text, or nothing is typed.
				p = input.DispatchKeyEvent(input.KeyDown).WithKey(ev.Key).WithCode(ev.Code).
					WithText(ev.Key).WithModifiers(input.Modifier(ev.Modifiers))
			}
			return p.Do(ctx)
		case "text":
			return input.InsertText(ev.Text).Do(ctx)
		}
		return fmt.Errorf("strava: unknown input type %q", ev.Type)
	}))
}

func mouseButton(b string) input.MouseButton {
	switch b {
	case "right":
		return input.Right
	case "middle":
		return input.Middle
	}
	return input.Left
}

// stealthJS runs before any page script. It hides the automation markers a
// login page such as Google's looks at, and removes WebAuthn: passkey prompts
// open a native dialog on the server's Chrome, which the user can neither see
// nor answer, and with the API gone Google offers password sign-in instead.
const stealthJS = `
Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
Object.defineProperty(navigator, 'languages', {get: () => ['en-US', 'en']});
if (!window.chrome) { window.chrome = {runtime: {}, app: {}}; }
try { delete window.PublicKeyCredential; } catch (e) {}
try { Object.defineProperty(window, 'PublicKeyCredential', {value: undefined, configurable: true}); } catch (e) {}
try { if (navigator.credentials) { navigator.credentials.get = () => Promise.reject(new DOMException('', 'NotAllowedError')); } } catch (e) {}
`

// maskAutomation presents the browser as the ordinary desktop Chrome it is:
// no "Headless" in the user agent or client hints, matching the real version.
func (s *RemoteSession) maskAutomation() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if _, err := page.AddScriptToEvaluateOnNewDocument(stealthJS).Do(ctx); err != nil {
			return err
		}
		_, product, _, ua, _, err := browser.GetVersion().Do(ctx)
		if err != nil {
			return nil // keep the configured user agent
		}
		ua = strings.ReplaceAll(ua, "HeadlessChrome", "Chrome")
		full := strings.TrimPrefix(strings.TrimPrefix(product, "HeadlessChrome/"), "Chrome/")
		major, _, _ := strings.Cut(full, ".")
		return emulation.SetUserAgentOverride(ua).
			WithAcceptLanguage("en-US,en").
			WithPlatform("Linux x86_64").
			WithUserAgentMetadata(&emulation.UserAgentMetadata{
				Brands: []*emulation.UserAgentBrandVersion{
					{Brand: "Not.A/Brand", Version: "24"},
					{Brand: "Chromium", Version: major},
					{Brand: "Google Chrome", Version: major},
				},
				FullVersionList: []*emulation.UserAgentBrandVersion{{Brand: "Chromium", Version: full}, {Brand: "Google Chrome", Version: full}}, Platform: "Linux", PlatformVersion: "6.0.0",
				Architecture: "x86", Model: "", Mobile: false,
			}).Do(ctx)
	})
}
