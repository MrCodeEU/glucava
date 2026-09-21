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
	"time"

	"github.com/starfederation/datastar-go/datastar"
	g "maragu.dev/gomponents"

	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/stats"
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
	info := SessionInfo{}
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
	return DashData{Acts: acts, Unit: render.Unit(cfg.Unit), Loc: s.loc(), Now: s.now(), Session: s.sessionInfo()}, nil
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
	samples, err := s.Store.LoadSamples(ctx, s.SourceName, act.Start.Add(-pre), act.End().Add(post))
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
	s.html(w, http.StatusOK, SettingsPage(s.page(r, "Settings", "settings"), SettingsData{
		Cfg: cfg, HasDexcomPassword: has(secrets.NameDexcomPassword),
		HasNtfyToken: has(secrets.NameNtfyToken), HasWebhookSecret: has(secrets.NameWebhookSecret),
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
	if s.Signal.Kick() {
		s.toast(sse, "ok", "Checking Strava now.")
		return
	}
	s.toast(sse, "", "A check is already waiting to run.")
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
	Lang           string  `json:"lang"`
	DexcomRegion   string  `json:"dexcomRegion"`
	DexcomUsername string  `json:"dexcomUsername"`
	DexcomPassword string  `json:"dexcomPassword"`
	NtfyURL        string  `json:"ntfyURL"`
	NtfyToken      string  `json:"ntfyToken"`
	WebhookURL     string  `json:"webhookURL"`
	WebhookSecret  string  `json:"webhookSecret"`
	RetentionDays  int     `json:"retentionDays"`
}

// validate returns a message for the first problem, or "".
func (v settingsSignals) validate() string {
	switch {
	case v.Unit != "mg/dL" && v.Unit != "mmol/L":
		return "Unit must be mg/dL or mmol/L."
	case v.RangeLow < 40 || v.RangeLow > 200:
		return "Target low must be between 40 and 200 mg/dL."
	case v.RangeHigh <= v.RangeLow || v.RangeHigh > 400:
		return "Target high must be above the low value and at most 400 mg/dL."
	case v.PreMin < 0 || v.PreMin > 240 || v.PostMin < 0 || v.PostMin > 240:
		return "Minutes before and after must be between 0 and 240."
	case v.PollMin < 1 || v.PollMin > 1440:
		return "The polling interval must be between 1 and 1440 minutes."
	case v.RetentionDays < 0 || v.RetentionDays > 3650:
		return "Retention must be between 0 and 3650 days."
	case v.Lang != "en" && v.Lang != "de":
		return "Language must be English or Deutsch."
	case v.DexcomRegion != "us" && v.DexcomRegion != "ous" && v.DexcomRegion != "jp":
		return "Choose a Dexcom region."
	case !validOptionalURL(v.NtfyURL):
		return "The ntfy URL must start with http:// or https://."
	case !validOptionalURL(v.WebhookURL):
		return "The webhook URL must start with http:// or https://."
	}
	return ""
}

func validOptionalURL(s string) bool {
	if s == "" {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

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
	cfg.PreMin, cfg.PostMin, cfg.PollMin, cfg.Lang = v.PreMin, v.PostMin, v.PollMin, v.Lang
	cfg.DexcomRegion, cfg.DexcomUsername = v.DexcomRegion, v.DexcomUsername
	cfg.NtfyURL, cfg.WebhookURL = v.NtfyURL, v.WebhookURL
	cfg.RetentionDays = v.RetentionDays
	if err := s.Store.SaveConfig(cfg); err != nil {
		s.toast(sse, "error", "Could not save: "+err.Error())
		return
	}

	// Secrets are write-only: an empty field keeps the stored value.
	for name, val := range map[string]string{
		secrets.NameDexcomPassword: v.DexcomPassword,
		secrets.NameNtfyToken:      v.NtfyToken,
		secrets.NameWebhookSecret:  v.WebhookSecret,
	} {
		if val == "" {
			continue
		}
		if err := s.Vault.Set(name, val); err != nil {
			s.toast(sse, "error", "Could not store a secret: "+err.Error())
			return
		}
	}
	_ = sse.PatchSignals([]byte(`{"dexcomPassword":"","ntfyToken":"","webhookSecret":""}`))
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
