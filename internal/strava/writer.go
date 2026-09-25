// Package strava updates Strava activity descriptions through a headless browser
// logged in with the user's own session cookies. Strava's API is not used.
package strava

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/MrCodeEU/glucava/internal/jobs"
)

// ErrSessionExpired means Strava redirected to the login page.
var ErrSessionExpired = jobs.ErrSessionExpired

// SelectorError means an element could not be found with any known selector.
// The LLM repair step keys off this type.
type SelectorError struct {
	Key   string   // "description" or "save"
	Tried []string // selectors that matched nothing
	URL   string
}

// Permanent tells the job queue that retrying will not help.
func (e *SelectorError) Permanent() bool { return true }

func (e *SelectorError) Error() string {
	return fmt.Sprintf("strava: no %s element on %s (tried %s)", e.Key, e.URL, strings.Join(e.Tried, ", "))
}

// Selectors lists CSS candidates in priority order. The first match wins.
// These are unverified guesses at Strava's edit page; adjust them after checking
// the real page, and prefer stable attributes such as name and aria-label.
type Selectors struct {
	Description []string
	Photo       []string // the file input that takes a picture
	Save        []string // used when the description field has no form with a submit button
}

// DefaultSelectors are the built-in candidates.
var DefaultSelectors = Selectors{
	Description: []string{
		`textarea[name="activity[description]"]`,
		`#activity_description`,
		`textarea[name*="description" i]`,
		`textarea[aria-label*="escription" i]`,
		// Strava's edit page (checked 2026-09) renders the description
		// textarea with no id or name at all, and its aria-label is
		// localized (e.g. German "Wie ist es gelaufen?"), so none of the
		// selectors above match on a non-English account. The one other
		// textarea on the page is the private note, which does keep a
		// stable id; picking "the textarea that isn't that one" works
		// regardless of locale, as long as the page has exactly these two.
		`textarea:not(#activity_private_note)`,
	},
	// The edit page's uploader (checked 2026-09) is a dropzone with a hidden
	// <input type=file multiple>; its class names carry a build hash, so match
	// only the stable prefix. "glucava strava inspect" lists the file inputs.
	Photo: []string{
		`[class*="MediaUploader--dropzone"] input[type="file"]`,
		`[class*="MediaUploader"] input[type="file"]`,
		`input[type="file"][accept*="image"]`,
		`input[type="file"]`,
	},
	Save: []string{
		`form button[type="submit"]`,
		`form input[type="submit"]`,
	},
}

// Config configures a Writer.
type Config struct {
	BaseURL    string // default https://www.strava.com
	ChromePath string // default: CHROME_PATH, then chromium/chrome on PATH
	NoSandbox  bool   // needed when Chrome runs as root, e.g. in Docker
	UserAgent  string
	Location   *time.Location // zone of times Strava reports without one; default time.Local

	// LoadCookies returns the stored session cookies.
	LoadCookies func() ([]Cookie, error)
	// SaveCookies, if set, receives the browser's cookies after a successful run,
	// so a session that Strava rotates stays valid.
	SaveCookies func([]Cookie) error

	Selectors      Selectors
	LoginSelectors LoginSelectors // unverified guesses at the login form; see DefaultLoginSelectors
	Pause          func()         // called between steps to look less robotic; nil means no pause
	Timeout        time.Duration  // whole run; default 2 minutes
	LocateTimeout  time.Duration  // wait for an element; default 15 seconds
	ProbeWait      time.Duration  // how long the photo probe watches the page per method; default 8 seconds
	UploadWait     time.Duration  // longest wait for the chosen photo to show up in the uploader; default 30 seconds
	SaveTimeout    time.Duration  // wait for the page to leave /edit after saving; default 15 seconds
	StartTimeout   time.Duration  // wait for Chrome to come up; default 60 seconds (chromedp's own 20 is too short on a busy or slow host)
}

// Writer implements jobs.Writer with chromedp.
type Writer struct {
	cfg Config
	mu  sync.Mutex // one browser at a time: Strava sees a single session
}

var (
	_ jobs.Writer      = (*Writer)(nil)
	_ jobs.PhotoWriter = (*Writer)(nil)
)

// NewWriter returns a Writer with defaults applied.
func NewWriter(cfg Config) *Writer {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://www.strava.com"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}
	if cfg.LocateTimeout <= 0 {
		cfg.LocateTimeout = 15 * time.Second
	}
	if cfg.SaveTimeout <= 0 {
		cfg.SaveTimeout = 15 * time.Second
	}
	if cfg.ProbeWait <= 0 {
		cfg.ProbeWait = 8 * time.Second
	}
	if cfg.UploadWait <= 0 {
		cfg.UploadWait = 30 * time.Second
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = 60 * time.Second
	}
	if len(cfg.Selectors.Description) == 0 {
		cfg.Selectors.Description = DefaultSelectors.Description
	}
	if len(cfg.Selectors.Photo) == 0 {
		cfg.Selectors.Photo = DefaultSelectors.Photo
	}
	if len(cfg.Selectors.Save) == 0 {
		cfg.Selectors.Save = DefaultSelectors.Save
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
	}
	return &Writer{cfg: cfg}
}

var idRe = regexp.MustCompile(`^\d+$`)

// UpdateDescription implements jobs.Writer. It reads the current description,
// applies merge, saves, then reloads the page to confirm the text was stored.
func (w *Writer) UpdateDescription(ctx context.Context, stravaID string, merge func(string) string) error {
	if !idRe.MatchString(stravaID) {
		return fmt.Errorf("strava: invalid activity id %q", stravaID)
	}
	editURL := w.cfg.BaseURL + "/activities/" + stravaID + "/edit"

	return w.withBrowser(ctx, func(ctx context.Context) error {
		sel, err := w.openEdit(ctx, editURL)
		if err != nil {
			return err
		}
		existing, err := evalString(ctx, `document.querySelector(`+jsStr(sel)+`).value`)
		if err != nil {
			return err
		}
		if existing == "" {
			// The page may fill the field after load. Read again before treating it as empty.
			w.pause()
			if existing, err = evalString(ctx, `document.querySelector(`+jsStr(sel)+`).value`); err != nil {
				return err
			}
		}
		want := merge(existing)
		if normalize(want) == normalize(existing) {
			return nil // already up to date
		}

		w.pause()
		if _, err := evalString(ctx, setValueJS(sel, want)); err != nil {
			return err
		}
		w.pause()
		if err := w.save(ctx, sel, editURL); err != nil {
			return err
		}

		// Confirm the server stored the text.
		sel, err = w.openEdit(ctx, editURL)
		if err != nil {
			return err
		}
		got, err := evalString(ctx, `document.querySelector(`+jsStr(sel)+`).value`)
		if err != nil {
			return err
		}
		if normalize(got) != normalize(want) {
			return errors.New("strava: verification failed: saved description differs from the intended text")
		}
		return w.exportCookies(ctx)
	})
}

// UploadPhoto implements jobs.PhotoWriter. It hands png to the edit page's
// media uploader the way a chosen file arrives (see photoInjectJS), waits for
// the uploader to send it to Strava's storage, saves the form, and checks that
// the reopened edit page lists one more photo. Anything short of that is an
// error, and none of those errors is retried: the photo may have arrived.
func (w *Writer) UploadPhoto(ctx context.Context, stravaID, name string, png []byte) error {
	if !idRe.MatchString(stravaID) {
		return fmt.Errorf("strava: invalid activity id %q", stravaID)
	}
	editURL := w.cfg.BaseURL + "/activities/" + stravaID + "/edit"

	return w.withBrowser(ctx, func(ctx context.Context) error {
		descSel, err := w.openEdit(ctx, editURL)
		if err != nil {
			return err
		}
		photoSel, err := w.locate(ctx, "photo", editURL, w.cfg.Selectors.Photo)
		if err != nil {
			return err
		}
		before, err := mediaOnPage(ctx)
		if err != nil {
			return permanentErr{err}
		}
		net := watchRequests(ctx)

		w.pause()
		if res, err := evalString(ctx, photoInjectJS("input-change-event", photoSel, png)); err != nil || res != "ok" {
			return permanentErr{fmt.Errorf("strava: hand the photo to the uploader: %v %s", err, res)}
		}
		// The uploader sends the file to storage as soon as it has it. Saving
		// before that finishes would drop it.
		if err := w.waitUploaded(ctx, net, w.cfg.UploadWait); err != nil {
			log.Printf("strava: photo upload did not finish; requests seen: %s; page: %s", net.filtered(), photoState(ctx))
			return permanentErr{fmt.Errorf("strava: the uploader did not send the photo within %s (nothing was saved; the page's uploader may have changed)", w.cfg.UploadWait)}
		}

		w.pause()
		if err := w.save(ctx, descSel, editURL); err != nil {
			return err
		}

		// Confirm: the reopened edit page lists the photo among the activity's media.
		deadline := time.Now().Add(w.cfg.LocateTimeout)
		for {
			if _, err := w.openEdit(ctx, editURL); err != nil {
				return err
			}
			if n, err := mediaOnPage(ctx); err == nil && n > before {
				break
			}
			if time.Now().After(deadline) {
				log.Printf("strava: after saving, requests seen: %s; reopened page: %s", net.filtered(), photoState(ctx))
				return permanentErr{errors.New("strava: saved, but the reopened edit page lists no new photo; check the activity on Strava before trying again, or a second copy may be added")}
			}
			if err := sleep(ctx, 2*time.Second); err != nil {
				return err
			}
		}
		return w.exportCookies(ctx)
	})
}

// mediaOnPage returns how many photos and videos the edit page lists for the
// activity, read from the props of its React media uploader.
func mediaOnPage(ctx context.Context) (int, error) {
	s, err := evalString(ctx, `(function(){
	  const el=document.querySelector('[data-react-class="MediaUploader"]');
	  if(!el)return 'no uploader';
	  try{const m=JSON.parse(el.getAttribute('data-react-props')).media;
	      return Array.isArray(m)?String(m.length):'no media list'}catch(e){return 'bad props'}})()`)
	if err != nil {
		return 0, err
	}
	n, cerr := strconv.Atoi(s)
	if cerr != nil {
		return 0, fmt.Errorf("strava: cannot read the activity's media from the edit page (%s)", s)
	}
	return n, nil
}

// waitUploaded polls until the uploader has registered the photo and sent it
// to storage.
func (w *Writer) waitUploaded(ctx context.Context, net *requestLog, max time.Duration) error {
	deadline := time.Now().Add(max)
	for !net.uploaded() {
		if time.Now().After(deadline) {
			return errors.New("no upload")
		}
		if err := sleep(ctx, 300*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

// uploaded reports whether two PUTs succeeded, one of them to /photos/metadata:
// the uploader registering the photo, then sending its bytes to storage.
func (l *requestLog) uploaded() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	ok, meta := 0, false
	for _, line := range l.lines {
		if strings.HasPrefix(line, "PUT ") && strings.HasSuffix(line, "-> 200") {
			ok++
			meta = meta || strings.Contains(line, "/photos/metadata")
		}
	}
	return ok >= 2 && meta
}

// permanentErr marks a failure that the job queue must not retry: an upload
// that may or may not have arrived is not safe to repeat blindly.
type permanentErr struct{ error }

func (permanentErr) Permanent() bool { return true }
func (e permanentErr) Unwrap() error { return e.error }

// photoStateJS describes the uploader area for diagnosing a photo that does not
// arrive: its markup, dialogs, visible buttons and the first images.
const photoStateJS = `JSON.stringify((function(){
  const cut=(s,n)=>(s||'').replace(/\s+/g,' ').trim().slice(0,n);
  const up=document.querySelector('[class*="MediaUploader"]');
  const box=up&&(up.parentElement&&up.parentElement.parentElement||up.parentElement||up);
  const vis=e=>!!(e.offsetWidth||e.offsetHeight||e.getClientRects().length);
  return {url:location.pathname,
    uploader:box?cut(box.outerHTML,2500):'(no MediaUploader element)',
    dialogs:[...document.querySelectorAll('[role=dialog],dialog')].filter(vis).map(e=>cut(e.innerText,300)),
    buttons:[...document.querySelectorAll('button')].filter(vis).map(e=>cut(e.innerText||e.getAttribute('aria-label'),40)).filter(Boolean).slice(0,30),
    images:[...document.querySelectorAll('img')].slice(0,12).map(e=>cut(e.currentSrc||e.src,90))}
})())`

func photoState(ctx context.Context) string {
	s, err := evalString(ctx, photoStateJS)
	if err != nil {
		return "(could not read the page: " + err.Error() + ")"
	}
	return s
}

// PhotoTry is what the page did after one way of handing it the file.
type PhotoTry struct {
	Method     string
	Requests   string // uploads the page made, without analytics and error reporting
	Blobs      int    // preview images/videos made from the chosen file
	Dialogs    string
	State      string
	Screenshot []byte
}

// noiseHosts are trackers the edit page talks to all the time.
var noiseHosts = []string{"snowplowanalytics", "google-analytics", "googlesyndication", "sentry.io", "doubleclick", "googletagmanager"}

func (l *requestLog) filtered() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var keep []string
	for _, line := range l.lines {
		noise := false
		for _, h := range noiseHosts {
			if strings.Contains(line, h) {
				noise = true
			}
		}
		if !noise {
			keep = append(keep, line)
		}
	}
	if len(keep) == 0 {
		return "none"
	}
	return strings.Join(keep, "; ")
}

// photoMethods are the ways to hand a file to a page's uploader. The real
// one depends on how the page's script listens for it.
var photoMethods = []string{"cdp-set-files", "input-change-event", "drop-event"}

// photoInjectJS gives the uploader the file without a real click: it builds a
// File in the page and either sets it on the input and fires change, or drops
// it on the dropzone like a drag and drop would.
func photoInjectJS(method, sel string, png []byte) string {
	return `(function(sel,b64,mode){
	  const bin=atob(b64), u=new Uint8Array(bin.length);
	  for(let i=0;i<bin.length;i++)u[i]=bin.charCodeAt(i);
	  const f=new File([u],'glucava.png',{type:'image/png'});
	  const dt=new DataTransfer(); dt.items.add(f);
	  const el=document.querySelector(sel); if(!el)return 'no input';
	  if(mode==='input-change-event'){
	    el.files=dt.files;
	    el.dispatchEvent(new Event('input',{bubbles:true}));
	    el.dispatchEvent(new Event('change',{bubbles:true}));
	    return 'ok';
	  }
	  const zone=el.closest('[role=presentation]')||el.parentElement;
	  for(const t of ['dragenter','dragover','drop']){
	    zone.dispatchEvent(new DragEvent(t,{bubbles:true,cancelable:true,dataTransfer:dt}));
	  }
	  return 'ok'})(` + jsStr(sel) + `,` + jsStr(base64.StdEncoding.EncodeToString(png)) + `,` + jsStr(method) + `)`
}

// ProbePhoto is a dry run of the photo upload. For each way of handing the
// page a file it loads the edit page fresh, hands over png, waits, and reports
// what the page did. It never saves the form. The uploader itself may already
// send the file to Strava.
func (w *Writer) ProbePhoto(ctx context.Context, stravaID string, png []byte) ([]PhotoTry, error) {
	if !idRe.MatchString(stravaID) {
		return nil, fmt.Errorf("strava: invalid activity id %q", stravaID)
	}
	editURL := w.cfg.BaseURL + "/activities/" + stravaID + "/edit"
	dir, err := os.MkdirTemp("", "glucava-photo-")
	if err != nil {
		return nil, err
	}
	defer removeDir(dir)
	file := filepath.Join(dir, "probe.png")
	if err := os.WriteFile(file, png, 0o600); err != nil {
		return nil, err
	}
	var out []PhotoTry
	err = w.withBrowser(ctx, func(ctx context.Context) error {
		net := watchRequests(ctx)
		for _, method := range photoMethods {
			if _, err := w.openEdit(ctx, editURL); err != nil {
				return err
			}
			sel, err := w.locate(ctx, "photo", editURL, w.cfg.Selectors.Photo)
			if err != nil {
				return err
			}
			if err := sleep(ctx, w.cfg.ProbeWait/4); err != nil { // let the page finish loading
				return err
			}
			net.reset()
			if method == "cdp-set-files" {
				if err := chromedp.Run(ctx, chromedp.SetUploadFiles(sel, []string{file}, chromedp.ByQuery)); err != nil {
					return err
				}
			} else if res, err := evalString(ctx, photoInjectJS(method, sel, png)); err != nil || res != "ok" {
				return fmt.Errorf("strava: %s: %v %s", method, err, res)
			}
			if err := sleep(ctx, w.cfg.ProbeWait); err != nil {
				return err
			}
			t := PhotoTry{Method: method, Requests: net.filtered(), State: photoState(ctx)}
			blobs, _ := evalString(ctx, `String(document.querySelectorAll('img[src^="blob:"],video[src^="blob:"],img[src^="data:image"]').length)`)
			t.Blobs, _ = strconv.Atoi(blobs)
			t.Dialogs, _ = evalString(ctx, `JSON.stringify([...document.querySelectorAll('[role=dialog],dialog,[class*="odal"]')].filter(e=>e.offsetWidth||e.offsetHeight).map(e=>(e.innerText||'').replace(/\s+/g,' ').trim().slice(0,300)))`)
			_ = chromedp.Run(ctx, chromedp.FullScreenshot(&t.Screenshot, 80))
			out = append(out, t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (l *requestLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = nil
	l.byID = map[network.RequestID]int{}
}

// requestLog remembers the uploads the page makes, for diagnosing a photo that
// does not arrive.
type requestLog struct {
	mu    sync.Mutex
	lines []string
	byID  map[network.RequestID]int
}

func watchRequests(ctx context.Context) *requestLog {
	l := &requestLog{byID: map[network.RequestID]int{}}
	_ = chromedp.Run(ctx, network.Enable())
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			if e.Request.Method == "GET" || e.Request.Method == "OPTIONS" || e.Request.Method == "HEAD" {
				return
			}
			u := e.Request.URL
			if i := strings.IndexAny(u, "?#"); i >= 0 {
				u = u[:i]
			}
			l.mu.Lock()
			l.byID[e.RequestID] = len(l.lines)
			l.lines = append(l.lines, e.Request.Method+" "+u)
			l.mu.Unlock()
		case *network.EventResponseReceived:
			l.mu.Lock()
			if i, ok := l.byID[e.RequestID]; ok {
				l.lines[i] += " -> " + strconv.Itoa(int(e.Response.Status))
			}
			l.mu.Unlock()
		}
	})
	return l
}

func (l *requestLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lines) == 0 {
		return "none"
	}
	return strings.Join(l.lines, "; ")
}

// LoginSelectors are unverified guesses at Strava's login form.
type LoginSelectors struct {
	Email    []string
	Password []string
	Submit   []string
}

// DefaultLoginSelectors are the built-in candidates.
var DefaultLoginSelectors = LoginSelectors{
	Email:    []string{`input[name="email"]`, `#email`, `input[type="email"]`},
	Password: []string{`input[name="password"]`, `#password`, `input[type="password"]`},
	Submit:   []string{`#login-button`, `button[type="submit"]`, `input[type="submit"]`},
}

// LoginBlocked explains why an automatic sign-in stopped instead of guessing.
type LoginBlocked string

const (
	// BlockedCredentials means Strava rejected the email or password.
	BlockedCredentials LoginBlocked = "invalid_credentials"
	// BlockedChallenge means Strava asked for something Glucava will not attempt:
	// a CAPTCHA, an SMS/email code, or a "verify this device" step.
	BlockedChallenge LoginBlocked = "challenge"
	// BlockedUnknown means the page did not reach the dashboard and matched
	// nothing recognised within the timeout.
	BlockedUnknown LoginBlocked = "unknown"
)

// LoginError is returned by Login when it stops instead of writing cookies.
type LoginError struct {
	Reason LoginBlocked
	Detail string
}

func (e *LoginError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("strava: sign-in stopped (%s): %s", e.Reason, e.Detail)
	}
	return fmt.Sprintf("strava: sign-in stopped (%s)", e.Reason)
}

// challengeMarkers are page fragments that mean "do not proceed automatically".
// Unverified: Strava's actual DOM for these flows has not been observed.
var challengeMarkers = []string{
	"captcha", "recaptcha", "hcaptcha", "verify it's you", "verification code",
	"check your email", "check your phone", "enter the code", "two-factor", "2fa",
}

// Login is EXPERIMENTAL and unverified against the real strava.com login form.
// It fills the login form and submits it once. It never guesses past a
// CAPTCHA, a verification code, or a wrong-credentials message — those stop
// with a LoginError so the caller can fall back to cookie import. On success
// it hands the resulting session cookies to SaveCookies; nothing is persisted
// on failure, and the password is never written anywhere by this function.
func (w *Writer) Login(ctx context.Context, email, password string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	sel := w.cfg.LoginSelectors
	if len(sel.Email) == 0 {
		sel = DefaultLoginSelectors
	}

	return w.withFreshBrowser(ctx, func(ctx context.Context) error {
		if err := chromedp.Run(ctx, chromedp.Navigate(w.cfg.BaseURL+"/login")); err != nil {
			return fmt.Errorf("strava: open login page: %w", err)
		}

		emailSel, err := w.locate(ctx, "login-email", w.cfg.BaseURL+"/login", sel.Email)
		if err != nil {
			return err
		}
		pwSel, err := w.locate(ctx, "login-password", w.cfg.BaseURL+"/login", sel.Password)
		if err != nil {
			return err
		}
		if _, err := evalString(ctx, setValueJS(emailSel, email)); err != nil {
			return err
		}
		if _, err := evalString(ctx, setValueJS(pwSel, password)); err != nil {
			return err
		}
		w.pause()

		submit := sel.Submit
		if len(submit) == 0 {
			submit = DefaultLoginSelectors.Submit
		}
		clickJS := `(function(cands){for(const s of cands){const b=document.querySelector(s);if(b){b.click();return 'true'}}return ''})(` + jsJSON(submit) + `)`
		clicked, err := evalString(ctx, clickJS)
		if err != nil {
			return err
		}
		if clicked != "true" {
			return &SelectorError{Key: "login-submit", Tried: submit, URL: w.cfg.BaseURL + "/login"}
		}

		deadline := time.Now().Add(w.cfg.LocateTimeout)
		for {
			var loc string
			if err := chromedp.Run(ctx, chromedp.Location(&loc)); err == nil && !isLoginURL(loc) {
				return w.exportCookies(ctx)
			}
			body, err := evalString(ctx, `document.body ? document.body.innerText.slice(0,4000).toLowerCase() : ""`)
			if err == nil {
				for _, m := range challengeMarkers {
					if strings.Contains(body, m) {
						return &LoginError{Reason: BlockedChallenge, Detail: "Strava is asking for something beyond email and password (CAPTCHA, code or device check). Use cookie import instead."}
					}
				}
				if strings.Contains(body, "incorrect") || strings.Contains(body, "wrong") || strings.Contains(body, "invalid") {
					return &LoginError{Reason: BlockedCredentials, Detail: "Strava reported a problem with the email or password."}
				}
			}
			if time.Now().After(deadline) {
				return &LoginError{Reason: BlockedUnknown, Detail: "the page never left the login screen"}
			}
			if err := sleep(ctx, 300*time.Millisecond); err != nil {
				return err
			}
		}
	})
}

// ExplainLoginError turns the result of Login into a message for the UI.
func ExplainLoginError(err error) string {
	var le *LoginError
	if errors.As(err, &le) {
		return le.Detail
	}
	var se *SelectorError
	if errors.As(err, &se) {
		return "the login form did not look as expected; use cookie import instead."
	}
	return err.Error()
}

// CheckSession reports ErrSessionExpired when the stored cookies no longer log in.
func (w *Writer) CheckSession(ctx context.Context) error {
	return w.withBrowser(ctx, func(ctx context.Context) error {
		loc, err := w.navigate(ctx, w.cfg.BaseURL+"/dashboard")
		if err != nil {
			return err
		}
		if isLoginURL(loc) {
			return ErrSessionExpired
		}
		return w.exportCookies(ctx)
	})
}

// openEdit loads the edit page, checks the login, and returns the matching description selector.
func (w *Writer) openEdit(ctx context.Context, editURL string) (string, error) {
	loc, err := w.navigate(ctx, editURL)
	if err != nil {
		return "", err
	}
	if isLoginURL(loc) {
		return "", ErrSessionExpired
	}
	return w.locate(ctx, "description", loc, w.cfg.Selectors.Description)
}

func (w *Writer) navigate(ctx context.Context, u string) (string, error) {
	var loc string
	err := chromedp.Run(ctx, chromedp.Navigate(u), chromedp.Location(&loc))
	if err != nil {
		return "", fmt.Errorf("strava: open %s: %w", u, err)
	}
	return loc, nil
}

// locate polls until one of the candidates matches, then returns it.
func (w *Writer) locate(ctx context.Context, key, pageURL string, candidates []string) (string, error) {
	js := `(function(c){for(const s of c){try{if(document.querySelector(s))return s}catch(e){}}return ""})(` + jsJSON(candidates) + `)`
	deadline := time.Now().Add(w.cfg.LocateTimeout)
	for {
		found, err := evalString(ctx, js)
		if err != nil {
			return "", err
		}
		if found != "" {
			return found, nil
		}
		if time.Now().After(deadline) {
			return "", &SelectorError{Key: key, Tried: candidates, URL: pageURL}
		}
		if err := sleep(ctx, 200*time.Millisecond); err != nil {
			return "", err
		}
	}
}

// save clicks the submit button of the description's form and waits for the page to move on.
func (w *Writer) save(ctx context.Context, descSel, editURL string) error {
	js := `(function(sel,cands){
	  const el=document.querySelector(sel); const f=el.form||el.closest('form'); let b=null;
	  if(f){b=f.querySelector('button[type=submit],input[type=submit]')}
	  if(!b){for(const s of cands){b=document.querySelector(s);if(b)break}}
	  if(b){setTimeout(()=>b.click(),0);return 'click'}
	  if(f){setTimeout(()=>f.requestSubmit(),0);return 'submit'}
	  return ''})(` + jsStr(descSel) + `,` + jsJSON(w.cfg.Selectors.Save) + `)`
	how, err := evalString(ctx, js)
	if err != nil {
		return err
	}
	if how == "" {
		return &SelectorError{Key: "save", Tried: w.cfg.Selectors.Save, URL: editURL}
	}

	// Wait for the page to leave the edit URL. If it does not, the verification
	// step reports what went wrong.
	deadline := time.Now().Add(w.cfg.SaveTimeout)
	for time.Now().Before(deadline) {
		if err := sleep(ctx, 200*time.Millisecond); err != nil {
			return err
		}
		var loc string
		if err := chromedp.Run(ctx, chromedp.Location(&loc)); err != nil {
			continue // the page is navigating
		}
		if !strings.HasSuffix(strings.TrimRight(strings.SplitN(loc, "?", 2)[0], "/"), "/edit") {
			return nil
		}
	}
	return nil
}

// withBrowser starts Chrome, sets the stored cookies, and runs fn.
func (w *Writer) withBrowser(ctx context.Context, fn func(context.Context) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	cookies, err := w.cfg.LoadCookies()
	if err != nil {
		return err
	}
	if len(cookies) == 0 {
		return ErrSessionExpired
	}
	return w.withFreshBrowser(ctx, func(ctx context.Context) error {
		if err := chromedp.Run(ctx, w.setCookies(cookies)); err != nil {
			return fmt.Errorf("strava: set cookies: %w", err)
		}
		return fn(ctx)
	})
}

// withFreshBrowser starts Chrome with no cookies of its own and runs fn.
// Callers that need the stored session use withBrowser instead; this is for
// Login, which has no session yet.
func (w *Writer) withFreshBrowser(ctx context.Context, fn func(context.Context) error) error {
	path, err := w.chromePath()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()

	// The profile holds cookies while Chrome runs. Keep it in a private
	// directory of our own and delete it afterwards.
	profile, err := os.MkdirTemp("", "glucava-chrome-*")
	if err != nil {
		return fmt.Errorf("strava: create browser profile: %w", err)
	}
	defer removeDir(profile)

	opts := append([]chromedp.ExecAllocatorOption(nil), chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromedp.ExecPath(path), chromedp.UserAgent(w.cfg.UserAgent), chromedp.UserDataDir(profile),
		chromedp.WSURLReadTimeout(w.cfg.StartTimeout))
	if w.cfg.NoSandbox {
		opts = append(opts, chromedp.Flag("no-sandbox", true), chromedp.Flag("disable-dev-shm-usage", true))
	}
	chromeOut := &limitedWriter{max: 4096}
	opts = append(opts, chromedp.ModifyCmdFunc(func(c *exec.Cmd) { c.Stderr = chromeOut }))
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	bctx, cancelBrowser := chromedp.NewContext(allocCtx, chromedp.WithErrorf(quietErrorf))
	defer func() {
		cancelBrowser()
		_ = chromedp.Cancel(bctx) // waits for Chrome to exit before the profile is deleted
	}()

	if err := chromedp.Run(bctx, chromedp.Navigate("about:blank")); err != nil {
		if out := strings.TrimSpace(chromeOut.String()); out != "" {
			err = fmt.Errorf("%w (chrome: %s)", err, out)
		}
		return fmt.Errorf("strava: start browser: %w", err)
	}
	return fn(bctx)
}

func (w *Writer) setCookies(cookies []Cookie) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for _, c := range cookies {
			p := network.SetCookie(c.Name, c.Value).WithPath(pathOr(c.Path)).
				WithSecure(c.Secure).WithHTTPOnly(c.HTTPOnly)
			if c.Domain != "" {
				p = p.WithDomain(c.Domain)
			} else {
				p = p.WithURL(w.cfg.BaseURL)
			}
			if !c.Expires.IsZero() {
				exp := cdp.TimeSinceEpoch(c.Expires)
				p = p.WithExpires(&exp)
			}
			if err := p.Do(ctx); err != nil {
				return fmt.Errorf("set cookie %s: %w", c.Name, err)
			}
		}
		return nil
	})
}

// exportCookies hands the browser's current cookies to SaveCookies.
func (w *Writer) exportCookies(ctx context.Context) error {
	if w.cfg.SaveCookies == nil {
		return nil
	}
	var out []Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		got, err := network.GetCookies().WithURLs([]string{w.cfg.BaseURL}).Do(ctx)
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
	if err != nil {
		return fmt.Errorf("strava: read cookies: %w", err)
	}
	return w.cfg.SaveCookies(out)
}

func (w *Writer) chromePath() (string, error) {
	if w.cfg.ChromePath != "" {
		return w.cfg.ChromePath, nil
	}
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p, nil
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("strava: no Chrome or Chromium found; set CHROME_PATH")
}

func (w *Writer) pause() {
	if w.cfg.Pause != nil {
		w.cfg.Pause()
	}
}

// isLoginURL reports whether loc is a Strava login or session page.
func isLoginURL(loc string) bool {
	u, err := url.Parse(loc)
	if err != nil {
		return false
	}
	p := u.Path
	return strings.HasPrefix(p, "/login") || strings.HasPrefix(p, "/session")
}

func normalize(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

func pathOr(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

func evalString(ctx context.Context, expr string) (string, error) {
	var out string
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &out)); err != nil {
		return "", fmt.Errorf("strava: page script: %w", err)
	}
	return out, nil
}

func setValueJS(sel, val string) string {
	return `(function(sel,val){
	  const el=document.querySelector(sel);
	  const set=Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el),'value').set;
	  el.focus(); set.call(el,val);
	  el.dispatchEvent(new Event('input',{bubbles:true}));
	  el.dispatchEvent(new Event('change',{bubbles:true}));
	  return el.value})(` + jsStr(sel) + `,` + jsStr(val) + `)`
}

func jsStr(s string) string { return jsJSON(s) }

func jsJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// limitedWriter keeps the first max bytes of Chrome's stderr for error messages.
type limitedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if room := l.max - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (l *limitedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// removeDir deletes dir. Chrome helper processes can still write for a moment
// after the main process exits, so it retries until the directory is gone.
func removeDir(dir string) {
	for range 20 {
		_ = os.RemoveAll(dir)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// quietErrorf is chromedp's error logger without its "unhandled node event"
// lines, which report harmless DOM events it has no use for.
func quietErrorf(format string, args ...any) {
	if strings.Contains(format, "unhandled node event") {
		return
	}
	log.Printf("ERROR: "+format, args...)
}
