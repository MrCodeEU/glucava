package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/starfederation/datastar-go/datastar"
	g "maragu.dev/gomponents"

	"github.com/MrCodeEU/glucava/internal/glucose/importers"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
	"github.com/MrCodeEU/glucava/internal/strava"
)

var digits = regexp.MustCompile(`^\d+$`)

func (s *Server) html(w http.ResponseWriter, code int, n g.Node) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = n.Render(w)
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	log.Printf("web: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// ------------------------------------------------------------------- pages

func (s *Server) sessionInfo() SessionInfo {
	info := SessionInfo{CanFindActivity: s.FindActivity != nil}
	raw, ok, err := s.Vault.Get(secrets.NameStravaCookies)
	if err == nil && ok {
		if list, derr := strava.DecodeCookies(raw); derr == nil && len(list) > 0 {
			info.Configured = true
			info.HasSession = strava.HasSession(list)
			for _, c := range list {
				info.Cookies = append(info.Cookies, CookieInfo{Name: c.Name, Expires: c.Expires})
			}
			sort.Slice(info.Cookies, func(i, j int) bool { return info.Cookies[i].Name < info.Cookies[j].Name })
		}
	}
	s.mu.Lock()
	info.CheckedAt, info.CheckOK, info.CheckErr = s.lastCheck.at, s.lastCheck.ok, s.lastCheck.err
	s.mu.Unlock()
	if !info.Configured {
		info.CheckedAt, info.CheckErr = time.Time{}, ""
	}
	return info
}

func (s *Server) dashData(ctx context.Context) (DashData, error) {
	acts, err := s.Store.ListActivities(ctx, 50)
	if err != nil {
		return DashData{}, err
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return DashData{}, err
	}
	d := DashData{Acts: acts, Unit: render.Unit(cfg.Unit), Loc: s.loc(), Now: s.now(), Session: s.sessionInfo()}
	if s.LatestGlucose != nil {
		// Short timeout: a slow or unreachable source must not hold up the
		// whole page. A nil result just leaves the tile showing "-".
		gctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		sample, err := s.LatestGlucose(gctx)
		cancel()
		if err != nil {
			log.Printf("web: latest glucose: %v", err)
		}
		d.Latest = sample
	}
	return d, nil
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.dashData(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.html(w, http.StatusOK, DashboardPage(s.page(r, "Activities", "dashboard"), d))
}

func (s *Server) activityData(ctx context.Context, id string) (*ActivityData, error) {
	act, err := s.Store.Activity(ctx, id)
	if err != nil || act == nil {
		return nil, err
	}
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		return nil, err
	}
	pre, post := time.Duration(cfg.PreMin)*time.Minute, time.Duration(cfg.PostMin)*time.Minute
	// Any source: an activity's window may be covered by the live source, a
	// backfilled import, or both, depending on how old it is.
	samples, err := s.Store.LoadSamplesAny(ctx, act.Start.Add(-pre), act.End().Add(post))
	if err != nil {
		return nil, err
	}

	d := &ActivityData{Act: *act, Samples: samples, Cfg: cfg, Loc: s.loc(), Now: s.now()}
	if sum, ok := stats.Summarize(samples, stats.Range{Low: cfg.RangeLow, High: cfg.RangeHigh}); ok {
		d.Block = render.Block(sum, samples, render.Options{Unit: render.Unit(cfg.Unit)})
		if d.Act.Summary == nil {
			d.Act.Summary = &sum
		}
	}
	evs, err := s.Store.ListEvents(ctx, 200)
	if err != nil {
		return nil, err
	}
	for _, e := range evs {
		if e.StravaID == id {
			d.Events = append(d.Events, e)
		}
	}
	return d, nil
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	if !digits.MatchString(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	d, err := s.activityData(r.Context(), r.PathValue("id"))
	if err != nil {
		s.serverError(w, err)
		return
	}
	if d == nil {
		http.NotFound(w, r)
		return
	}
	s.html(w, http.StatusOK, ActivityPage(s.page(r, "Activity", "dashboard"), *d))
}

func (s *Server) stravaPage(w http.ResponseWriter, r *http.Request) {
	s.html(w, http.StatusOK, StravaPage(s.page(r, "Strava session", "strava"), s.sessionInfo()))
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.Store.LoadConfig()
	if err != nil {
		s.serverError(w, err)
		return
	}
	has := func(name string) bool { _, ok, _ := s.Vault.Get(name); return ok }
	q := r.URL.Query()
	s.html(w, http.StatusOK, SettingsPage(s.page(r, "Settings", "settings"), SettingsData{
		Cfg: cfg, HasDexcomPassword: has(secrets.NameDexcomPassword),
		HasNtfyToken: has(secrets.NameNtfyToken), HasWebhookSecret: has(secrets.NameWebhookSecret), HasSMTPPassword: has(secrets.NameSMTPPassword),
		ImportFormats: importers.Names(), ImportOK: q.Get("importOK"), ImportErr: q.Get("importErr"),
	}))
}

func (s *Server) tokensPage(w http.ResponseWriter, r *http.Request) {
	list, err := s.Tokens.List()
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.html(w, http.StatusOK, TokensPage(s.page(r, "Triggers", "tokens"), list, s.baseURL(r), s.loc()))
}

func (s *Server) eventsPage(w http.ResponseWriter, r *http.Request) {
	evs, err := s.Store.ListEvents(r.Context(), 200)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.html(w, http.StatusOK, EventsPage(s.page(r, "Notifications", "events"), evs, s.loc(), s.now()))
}

// ----------------------------------------------------------------- streams

// stream keeps an SSE connection open and calls render whenever the bus fires.
func (s *Server) stream(w http.ResponseWriter, r *http.Request, render func(*datastar.ServerSentEventGenerator) error) {
	sse := datastar.NewSSE(w, r)
	changes, cancel := s.Bus.Subscribe()
	defer cancel()
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			if err := render(sse); err != nil {
				return
			}
		case <-keepalive.C:
			if err := sse.PatchSignals([]byte("{}")); err != nil {
				return
			}
		}
	}
}

func (s *Server) streamLive(w http.ResponseWriter, r *http.Request) {
	s.stream(w, r, func(sse *datastar.ServerSentEventGenerator) error {
		d, err := s.dashData(r.Context())
		if err != nil {
			return err
		}
		return sse.PatchElements(renderString(LiveDash(d)))
	})
}

func (s *Server) streamActivity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !digits.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	s.stream(w, r, func(sse *datastar.ServerSentEventGenerator) error {
		d, err := s.activityData(r.Context(), id)
		if err != nil || d == nil {
			return errors.New("activity gone")
		}
		return sse.PatchElements(renderString(ActivityBody(*d)))
	})
}

// ----------------------------------------------------------------- actions

func (s *Server) toast(sse *datastar.ServerSentEventGenerator, variant, msg string) {
	_ = sse.PatchElements(renderString(Toast(variant, msg)),
		datastar.WithSelector("#toast"), datastar.WithModeAppend())
}

func (s *Server) actionPoll(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if s.Poll == nil {
		// No way to run it inline (e.g. demo mode); fall back to nudging the
		// background loop, with no result to report back.
		if s.Signal.Kick() {
			s.toast(sse, "ok", "Checking Strava now.")
			return
		}
		s.toast(sse, "", "A check is already waiting to run.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	n, err := s.Poll(ctx)

	if n, cerr := s.Store.CountRecentErrors(r.Context(), s.now().Add(-24*time.Hour)); cerr == nil {
		_ = sse.PatchElements(renderString(navAlertsBadge(n)))
	}

	if err != nil {
		log.Printf("web: check strava now: %v", err)
		msg := err.Error()
		if errors.Is(err, jobs.ErrSessionExpired) {
			msg = "the Strava session has expired; import fresh cookies."
		}
		s.toast(sse, "error", "Check failed: "+msg)
		return
	}
	if n > 0 {
		s.toast(sse, "ok", fmt.Sprintf("Checked. Queued %d new activit%s.", n, plural(n)))
	} else {
		s.toast(sse, "ok", "Checked. Nothing new.")
	}
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func (s *Server) actionReprocess(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if !digits.MatchString(r.PathValue("id")) {
		s.toast(sse, "error", "That is not an activity id.")
		return
	}
	act, err := s.Store.Activity(r.Context(), r.PathValue("id"))
	if err != nil || act == nil {
		s.toast(sse, "error", "That activity no longer exists.")
		return
	}
	act.Status, act.Error = jobs.StatusPending, ""
	if err := s.Store.SaveActivity(r.Context(), act); err != nil {
		s.toast(sse, "error", "Could not update the activity: "+err.Error())
		return
	}
	queued, err := s.Jobs.Enqueue(jobs.Job{Activity: *act, Force: true})
	switch {
	case err != nil:
		s.toast(sse, "error", "Could not queue it: "+err.Error())
	case !queued:
		s.toast(sse, "", "This activity is already queued.")
	default:
		s.toast(sse, "ok", "Queued for reprocessing.")
	}
}

// actionProcessActivity looks up an arbitrary Strava activity id (one the
// poller never queued, e.g. it predates this app or is older than its
// MaxAge) and queues it for processing, same as reprocessing an existing
// one. If the id is already in the store, it just reprocesses that row
// instead of looking it up on Strava again.
func (s *Server) actionProcessActivity(w http.ResponseWriter, r *http.Request) {
	var v struct {
		ActivityID string `json:"processActivityId"`
	}
	readErr := datastar.ReadSignals(r, &v)
	sse := datastar.NewSSE(w, r)
	id := strings.TrimSpace(v.ActivityID)
	if readErr != nil || !digits.MatchString(id) {
		s.toast(sse, "error", "Enter a numeric Strava activity id.")
		return
	}
	if s.FindActivity == nil {
		s.toast(sse, "error", "Looking up an activity by id is not available here.")
		return
	}

	act, err := s.Store.Activity(r.Context(), id)
	if err != nil {
		s.toast(sse, "error", "Could not look up that activity: "+err.Error())
		return
	}
	if act == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		found, ferr := s.FindActivity(ctx, id)
		switch {
		case errors.Is(ferr, jobs.ErrSessionExpired):
			s.toast(sse, "error", "The Strava session has expired; import fresh cookies.")
			return
		case ferr != nil:
			log.Printf("web: find activity %s: %v", id, ferr)
			s.toast(sse, "error", "Could not look it up: "+ferr.Error())
			return
		case found == nil:
			s.toast(sse, "error", "No activity with that id was found in your Strava training log.")
			return
		}
		act = found
	}
	act.Status, act.Error = jobs.StatusPending, ""
	if err := s.Store.SaveActivity(r.Context(), act); err != nil {
		s.toast(sse, "error", "Could not save the activity: "+err.Error())
		return
	}
	queued, err := s.Jobs.Enqueue(jobs.Job{Activity: *act, Force: true})
	switch {
	case err != nil:
		s.toast(sse, "error", "Could not queue it: "+err.Error())
	case !queued:
		s.toast(sse, "", "This activity is already queued.")
	default:
		_ = sse.PatchSignals([]byte(`{"processActivityId":""}`))
		s.toast(sse, "ok", `Queued "`+act.Name+`" for processing.`)
	}
}

func (s *Server) actionRestore(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if !digits.MatchString(r.PathValue("id")) {
		s.toast(sse, "error", "That is not an activity id.")
		return
	}
	act, err := s.Store.Activity(r.Context(), r.PathValue("id"))
	if err != nil || act == nil {
		s.toast(sse, "error", "That activity no longer exists.")
		return
	}
	if act.Original == nil {
		s.toast(sse, "error", "No original description was saved for this activity.")
		return
	}
	queued, err := s.Jobs.Enqueue(jobs.Job{Activity: *act, Restore: true})
	switch {
	case err != nil:
		s.toast(sse, "error", "Could not queue it: "+err.Error())
	case !queued:
		s.toast(sse, "", "This activity is already queued.")
	default:
		s.toast(sse, "ok", "Restoring the original description.")
	}
}

type settingsSignals struct {
	Unit           string  `json:"unit"`
	RangeLow       float64 `json:"rangeLow"`
	RangeHigh      float64 `json:"rangeHigh"`
	PreMin         int     `json:"preMin"`
	PostMin        int     `json:"postMin"`
	PollMin        int     `json:"pollMin"`
	DexcomRegion   string  `json:"dexcomRegion"`
	DexcomUsername string  `json:"dexcomUsername"`
	DexcomPassword string  `json:"dexcomPassword"`
	NtfyURL        string  `json:"ntfyURL"`
	NtfyToken      string  `json:"ntfyToken"`
	WebhookURL     string  `json:"webhookURL"`
	WebhookSecret  string  `json:"webhookSecret"`
	EmailTo        string  `json:"emailTo"`
	SMTPHost       string  `json:"smtpHost"`
	SMTPPort       int     `json:"smtpPort"`
	SMTPUsername   string  `json:"smtpUsername"`
	SMTPPassword   string  `json:"smtpPassword"`
	SMTPTLS        bool    `json:"smtpTLS"`
	SMTPSender     string  `json:"smtpSender"`
	SMTPSenderName string  `json:"smtpSenderName"`
	RetentionDays  int     `json:"retentionDays"`
	PublicURL      string  `json:"publicURL"`
	MailAlerts     bool    `json:"mailAlerts"`
	MailActivity   bool    `json:"mailActivity"`
	MailWeekly     bool    `json:"mailWeekly"`
	MailHealth     bool    `json:"mailHealth"`
	GapAlertHours  int     `json:"gapAlertHours"`
	ChartImage     bool    `json:"chartImage"`
}

// config converts the form values to the stored settings shape.
func (v settingsSignals) config() store.Config {
	return store.Config{
		Unit: v.Unit, RangeLow: v.RangeLow, RangeHigh: v.RangeHigh, PreMin: v.PreMin, PostMin: v.PostMin,
		PollMin: v.PollMin, DexcomRegion: v.DexcomRegion, DexcomUsername: v.DexcomUsername,
		NtfyURL: v.NtfyURL, WebhookURL: v.WebhookURL, EmailTo: v.EmailTo, RetentionDays: v.RetentionDays,
		SMTPHost: v.SMTPHost, SMTPPort: v.SMTPPort, SMTPUsername: v.SMTPUsername, SMTPTLS: v.SMTPTLS,
		SMTPSender: v.SMTPSender, SMTPSenderName: v.SMTPSenderName,
		PublicURL: strings.TrimSpace(v.PublicURL), MailAlerts: v.MailAlerts, MailActivity: v.MailActivity, MailWeekly: v.MailWeekly,
		MailHealth: v.MailHealth, GapAlertHours: v.GapAlertHours, ChartImage: v.ChartImage,
	}
}

// validate returns a message for the first problem, or "".
func (v settingsSignals) validate() string { return v.config().Validate() }

func (s *Server) actionSettings(w http.ResponseWriter, r *http.Request) {
	var v settingsSignals
	readErr := datastar.ReadSignals(r, &v) // must run before NewSSE, which takes over the request body
	sse := datastar.NewSSE(w, r)
	if readErr != nil {
		s.toast(sse, "error", "Could not read the form.")
		return
	}
	if msg := v.validate(); msg != "" {
		s.toast(sse, "error", msg)
		return
	}

	cfg, err := s.Store.LoadConfig()
	if err != nil {
		s.toast(sse, "error", err.Error())
		return
	}
	cfg.Unit, cfg.RangeLow, cfg.RangeHigh = v.Unit, v.RangeLow, v.RangeHigh
	cfg.PreMin, cfg.PostMin, cfg.PollMin = v.PreMin, v.PostMin, v.PollMin
	cfg.DexcomRegion, cfg.DexcomUsername = v.DexcomRegion, v.DexcomUsername
	cfg.NtfyURL, cfg.WebhookURL, cfg.EmailTo = v.NtfyURL, v.WebhookURL, v.EmailTo
	cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPTLS = v.SMTPHost, v.SMTPPort, v.SMTPUsername, v.SMTPTLS
	cfg.SMTPSender, cfg.SMTPSenderName = v.SMTPSender, v.SMTPSenderName
	cfg.RetentionDays = v.RetentionDays
	cfg.PublicURL, cfg.MailAlerts = strings.TrimSpace(v.PublicURL), v.MailAlerts
	cfg.MailActivity, cfg.MailWeekly = v.MailActivity, v.MailWeekly
	cfg.MailHealth, cfg.GapAlertHours = v.MailHealth, v.GapAlertHours
	cfg.ChartImage = v.ChartImage
	if err := s.Store.SaveConfig(cfg); err != nil {
		s.toast(sse, "error", "Could not save: "+err.Error())
		return
	}

	// Secrets are write-only: an empty field keeps the stored value.
	for name, val := range map[string]string{
		secrets.NameDexcomPassword: v.DexcomPassword,
		secrets.NameNtfyToken:      v.NtfyToken,
		secrets.NameWebhookSecret:  v.WebhookSecret,
		secrets.NameSMTPPassword:   v.SMTPPassword,
	} {
		if val == "" {
			continue
		}
		if err := s.Vault.Set(name, val); err != nil {
			s.toast(sse, "error", "Could not store a secret: "+err.Error())
			return
		}
	}
	_ = sse.PatchSignals([]byte(`{"dexcomPassword":"","ntfyToken":"","webhookSecret":"","smtpPassword":""}`))
	has := func(name string) bool { _, ok, _ := s.Vault.Get(name); return ok }
	_ = sse.PatchElements(renderString(DexcomSecretStatus(has(secrets.NameDexcomPassword))))
	_ = sse.PatchElements(renderString(NtfySecretStatus(has(secrets.NameNtfyToken))))
	_ = sse.PatchElements(renderString(WebhookSecretStatus(has(secrets.NameWebhookSecret))))
	_ = sse.PatchElements(renderString(SMTPSecretStatus(has(secrets.NameSMTPPassword))))
	s.toast(sse, "ok", "Settings saved.")
	s.Bus.Publish()
}

func (s *Server) actionNotifyTest(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if s.SendTest == nil {
		s.toast(sse, "error", "Notifications are not available here.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.SendTest(ctx); err != nil {
		s.toast(sse, "error", "Test failed: "+err.Error())
		return
	}
	s.toast(sse, "ok", "Test notification sent.")
}

func (s *Server) actionStravaCookies(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Cookies string `json:"cookies"`
	}
	readErr := datastar.ReadSignals(r, &v)
	sse := datastar.NewSSE(w, r)
	if readErr != nil {
		s.toast(sse, "error", "Could not read the form.")
		return
	}
	list, err := strava.ParseCookies(v.Cookies)
	if err != nil {
		s.toast(sse, "error", err.Error())
		return
	}
	enc, err := strava.EncodeCookies(list)
	if err == nil {
		err = s.Vault.Set(secrets.NameStravaCookies, enc)
	}
	if err != nil {
		s.toast(sse, "error", "Could not store the cookies: "+err.Error())
		return
	}
	s.mu.Lock()
	s.lastCheck.at, s.lastCheck.ok, s.lastCheck.err = time.Time{}, false, ""
	s.mu.Unlock()

	_ = sse.PatchSignals([]byte(`{"cookies":""}`))
	_ = sse.PatchElements(renderString(StravaStatusCard(s.sessionInfo())))
	s.toast(sse, "ok", fmt.Sprintf("Stored %d cookies.", len(list)))
	s.Bus.Publish()
}

// actionGlucoseImport is a plain HTML form post (not a Datastar action):
// file uploads are a poor fit for the JSON-signal request/SSE-response
// pattern the rest of the UI uses, and an ordinary multipart form needs no
// JavaScript. It redirects back to Settings with a one-shot flash message in
// the query string, the same way the plain login form reports its error.
func (s *Server) actionGlucoseImport(w http.ResponseWriter, r *http.Request) {
	// importFetch marks the request as coming from the progressive-enhancement
	// script (static/glucose-import.js), which submits via fetch so it can
	// swap in the result in place instead of a full page reload that leaves
	// you back at the top of Settings, scrolled away from what you just did.
	// A plain form post (no JS) instead redirects back with a flash message,
	// same as the login page's error handling.
	ajax := r.Header.Get("X-Glucava-Fetch") == "1"
	fail := func(msg string) {
		if ajax {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, renderString(GlucoseImportStatus("", msg)))
			return
		}
		http.Redirect(w, r, "/settings?importErr="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		fail("Could not read the upload: " + err.Error())
		return
	}
	format := r.FormValue("format")
	imp, ok := importers.Get(format)
	if !ok {
		fail(fmt.Sprintf("Unknown format %q.", format))
		return
	}
	source := r.FormValue("source")
	if source == "" {
		source = format
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		fail("Choose a file to import.")
		return
	}
	defer func() { _ = file.Close() }()

	if err := importers.CheckZipSupport(imp, file, format); err != nil {
		fail(err.Error())
		return
	}
	samples, skipped, err := imp.Parse(file)
	if err != nil {
		fail("Could not read the file: " + err.Error())
		return
	}
	if len(samples) == 0 {
		fail("No readings found in that file. Wrong format, or an empty export?")
		return
	}
	if err := s.Store.SaveSamples(r.Context(), source, samples); err != nil {
		log.Printf("web: glucose import: store: %v", err)
		fail("Could not store the readings.")
		return
	}
	log.Printf("web: glucose import: stored %d readings (%d skipped) as source %q", len(samples), skipped, source)

	msg := fmt.Sprintf("Stored %d readings as %q.", len(samples), source)
	if skipped > 0 {
		msg += fmt.Sprintf(" %d rows were skipped (not a glucose reading, or unparseable).", skipped)
	}
	if ajax {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, renderString(GlucoseImportStatus(msg, "")))
		return
	}
	http.Redirect(w, r, "/settings?importOK="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) actionStravaTest(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if s.Session == nil {
		s.toast(sse, "error", "Session testing is not available here.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	err := s.Session.CheckSession(ctx)

	s.mu.Lock()
	s.lastCheck.at, s.lastCheck.ok, s.lastCheck.err = s.now(), err == nil, ""
	if err != nil {
		s.lastCheck.err = err.Error()
		if errors.Is(err, jobs.ErrSessionExpired) {
			s.lastCheck.err = "Strava sent the browser to the login page: the cookies are no longer valid. Import fresh ones."
		}
	}
	s.mu.Unlock()

	_ = sse.PatchElements(renderString(StravaStatusCard(s.sessionInfo())))
	if err != nil {
		s.toast(sse, "error", "The session test failed.")
	} else {
		s.toast(sse, "ok", "Strava accepts the stored cookies.")
	}
	s.Bus.Publish()
}

func (s *Server) actionTokenCreate(w http.ResponseWriter, r *http.Request) {
	var v struct {
		TokenName string `json:"tokenName"`
	}
	readErr := datastar.ReadSignals(r, &v)
	sse := datastar.NewSSE(w, r)
	if readErr != nil {
		s.toast(sse, "error", "Could not read the form.")
		return
	}
	token, err := s.Tokens.Create(v.TokenName)
	if err != nil {
		s.toast(sse, "error", err.Error())
		return
	}
	list, _ := s.Tokens.List()
	_ = sse.PatchElements(renderString(SecretReveal(v.TokenName, token)))
	_ = sse.PatchElements(renderString(TokenList(list, s.loc())))
	_ = sse.PatchSignals([]byte(`{"tokenName":""}`))
}

func (s *Server) actionTokenRevoke(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if err := s.Tokens.Revoke(r.PathValue("name")); err != nil {
		s.toast(sse, "error", err.Error())
		return
	}
	list, _ := s.Tokens.List()
	_ = sse.PatchElements(renderString(TokenList(list, s.loc())))
	s.toast(sse, "ok", "Token revoked.")
}

func (s *Server) actionStravaLogin(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Email    string `json:"loginEmail"`
		Password string `json:"loginPassword"`
	}
	readErr := datastar.ReadSignals(r, &v)
	sse := datastar.NewSSE(w, r)
	if readErr != nil || v.Email == "" || v.Password == "" {
		s.toast(sse, "error", "Enter an email and password.")
		return
	}
	if s.StravaLogin == nil {
		s.toast(sse, "error", "Automatic sign-in is not available here.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	err := s.StravaLogin(ctx, v.Email, v.Password)
	v.Password = "" // best effort; the value also never leaves this function on our end

	s.mu.Lock()
	s.lastCheck.at, s.lastCheck.ok, s.lastCheck.err = s.now(), err == nil, ""
	if err != nil {
		s.lastCheck.err = strava.ExplainLoginError(err)
	}
	s.mu.Unlock()

	_ = sse.PatchElements(renderString(StravaStatusCard(s.sessionInfo())))
	if err != nil {
		log.Printf("web: strava automatic sign-in for %s: %v", v.Email, err)
		s.toast(sse, "error", "Automatic sign-in stopped: "+strava.ExplainLoginError(err))
		return
	}
	log.Printf("web: strava automatic sign-in for %s: succeeded", v.Email)
	s.toast(sse, "ok", "Signed in and stored the session.")
}

func (s *Server) actionDexcomTest(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if s.GlucoseTest == nil {
		s.toast(sse, "error", "Dexcom testing is not available here.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.GlucoseTest(ctx); err != nil {
		log.Printf("web: dexcom test connection: %v", err)
		s.toast(sse, "error", "Dexcom check failed: "+err.Error())
		return
	}
	log.Printf("web: dexcom test connection: accepted")
	s.toast(sse, "ok", "Dexcom accepted the stored credentials.")
}

func (s *Server) actionPurge(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Confirm string `json:"purgeConfirm"`
	}
	readErr := datastar.ReadSignals(r, &v)
	sse := datastar.NewSSE(w, r)
	if readErr != nil || v.Confirm != "DELETE" {
		s.toast(sse, "error", "Type DELETE to confirm.")
		return
	}
	n, err := s.Store.PurgeAll(r.Context())
	if err != nil {
		s.toast(sse, "error", "Could not delete: "+err.Error())
		return
	}
	s.toast(sse, "ok", fmt.Sprintf("Deleted %d readings, %d activities and %d events.", n.Samples, n.Activities, n.Events))
}

func (s *Server) exportSamples(w http.ResponseWriter, _ *http.Request) {
	s.csv(w, "glucava-readings.csv", s.Store.ExportSamples)
}

func (s *Server) exportActivities(w http.ResponseWriter, _ *http.Request) {
	s.csv(w, "glucava-activities.csv", s.Store.ExportActivities)
}

func (s *Server) csv(w http.ResponseWriter, name string, write func(io.Writer) error) {
	var buf bytes.Buffer // buffered so a failure can still return a proper error status
	if err := write(&buf); err != nil {
		s.serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}
