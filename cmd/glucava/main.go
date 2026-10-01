package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/spf13/cobra"

	"github.com/MrCodeEU/glucava/internal/bootstrap"
	"github.com/MrCodeEU/glucava/internal/bus"
	"github.com/MrCodeEU/glucava/internal/canary"
	"github.com/MrCodeEU/glucava/internal/clientip"
	"github.com/MrCodeEU/glucava/internal/demo"
	"github.com/MrCodeEU/glucava/internal/digest"
	"github.com/MrCodeEU/glucava/internal/gap"
	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/ingest"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/logging"
	"github.com/MrCodeEU/glucava/internal/metrics"
	_ "github.com/MrCodeEU/glucava/internal/migrations"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/poll"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
	"github.com/MrCodeEU/glucava/internal/strava"
	"github.com/MrCodeEU/glucava/internal/taskerprofile"
	"github.com/MrCodeEU/glucava/internal/tokens"
	"github.com/MrCodeEU/glucava/internal/trigger"
	"github.com/MrCodeEU/glucava/internal/web"
)

var buildID = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		runHealthcheck()
		return
	}
	app := pocketbase.New()
	changes := &bus.Bus{}

	// slog.SetDefault as early as possible, before anything else logs: every
	// log.Printf/slog call site anywhere in the app then also lands in
	// logHandler's ring buffer for the in-app /logs view, and wakes the same
	// live-update bus every other page already uses.
	logHandler := logging.NewHandler(slog.NewTextHandler(os.Stderr, nil), logging.DefaultCapacity, changes)
	slog.SetDefault(slog.New(logHandler))

	toks := &tokens.Manager{App: app}
	st := &store.PB{App: app, Changed: changes.Publish}
	signal := trigger.NewSignal()
	demoMode := os.Getenv("GLUCAVA_DEMO") == "1"

	bootstrap.EnforceSingleUser(app)
	// A failed command is not a usage error; skip the flag dump before the message.
	app.RootCmd.SilenceUsage = true
	app.RootCmd.Version = buildID
	app.RootCmd.AddCommand(tokenCommand(app, toks), stravaCommand(app), dexcomCommand(app), userCommand(app), secretsCommand(app), dataCommand(app, st), glucoseCommand(app, st), configCommand(app, st))

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if demoMode {
			demoDefaults()
		}
		proxies, err := clientip.Parse(os.Getenv("GLUCAVA_TRUSTED_PROXIES"))
		if err != nil {
			return err
		}
		if os.Getpid() == 1 {
			slog.Warn("glucava is PID 1 without an init, so Chrome's leftover processes are never reaped and will fill the container's pids limit; start the container with --init (compose: init: true) or use the official image, which has one")
		}
		if err := os.Chmod(app.DataDir(), 0o700); err != nil {
			slog.Error("chmod data dir", "err", err)
		}
		if os.Getenv("GLUCAVA_ADMIN_UI") != "1" {
			e.Router.BindFunc(blockPocketBase)
		}
		if err := bootstrap.SilenceSuperuserPrompt(app); err != nil {
			return err
		}
		if err := bootstrap.EnsureAdminUser(app); err != nil {
			return err
		}
		tun, err := loadTuning(os.Getenv)
		if err != nil {
			return err
		}
		vault, err := openVault(app)
		if err != nil {
			return err
		}
		if !secrets.KeyFromEnv() {
			slog.Warn("encryption key is stored on disk; keep it out of backups of the data dir, or set GLUCAVA_SECRET_KEY", "path", secrets.KeyFilePath(app.DataDir()))
		}
		if err := bootstrap.EnsureDexcomCredential(app, vault, secrets.NameDexcomPassword); err != nil {
			return err
		}
		if err := bootstrap.ApplyRetentionOverride(app); err != nil {
			return err
		}
		if err := bootstrap.EnsureSMTP(app, vault, secrets.NameSMTPPassword); err != nil {
			return err
		}
		if err := bootstrap.EnsurePublicURL(app); err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(context.Background())
		app.OnTerminate().BindFunc(func(te *core.TerminateEvent) error {
			cancel()
			return te.Next()
		})

		// The pipeline uses real Strava and Dexcom, or stand-ins in demo mode.
		var (
			writer       jobs.Writer
			source       glucose.Source
			lister       poll.Lister
			session      web.SessionChecker
			sourceName   string
			stravaLogin  func(ctx context.Context, email, password string) error
			findActivity func(ctx context.Context, stravaID string) (*jobs.Activity, error)
			inspector    canary.Inspector
		)
		if demoMode {
			if err := demo.Seed(ctx, st, time.Now()); err != nil {
				return fmt.Errorf("seed demo data: %w", err)
			}
			if err := storeDemoCookies(vault); err != nil {
				return err
			}
			writer, source, lister, session = &demo.Writer{Delay: 1500 * time.Millisecond}, demo.Source{}, &demo.Lister{Store: st}, demo.Session{}
			sourceName = demo.SourceName
		} else {
			sw := newStravaWriter(vault, tun)
			src, err := newSource(os.Getenv("GLUCAVA_SOURCE"), st, vault)
			if err != nil {
				return err
			}
			sourceName = src.name
			writer, source, lister, session = sw, src.source, sw, sw
			stravaLogin = sw.Login
			findActivity = sw.FindActivity
			inspector = sw
		}

		progress := jobs.NewProgress()
		proc := &jobs.Processor{Store: st, Source: source, SourceName: sourceName, Writer: writer, Progress: progress, Notify: changes.Publish}
		queue := jobs.NewQueue(proc, tun.RetryBackoff, 64)
		queue.OnDone = func(ctx context.Context, a jobs.Activity) {
			set, err := st.Settings(ctx)
			if err != nil {
				return
			}
			// Same window the description used, so the chart shows what was summarized.
			samples, _ := st.LoadSamplesAny(ctx, a.Start.Add(-set.Pre), a.End().Add(set.Post))
			if m, ok := digest.ActivityMessage(installTr(st).Get(), a, set.Unit, set.Range, samples, time.Local); ok {
				if err := sendSummary(ctx, st, vault, demoMode, m); err != nil {
					slog.Error("notify activity summary", "err", err)
				}
			}
		}
		if stuck, err := st.RecoverStuck(ctx); err != nil {
			slog.Error("recover stuck activities", "err", err)
		} else if len(stuck) > 0 {
			slog.Info("recovered activities left mid-run by a previous restart; reprocessing", "count", len(stuck))
			for _, a := range stuck {
				if _, err := queue.Enqueue(jobs.Job{Activity: a}); err != nil {
					slog.Error("re-enqueue", "activity", a.StravaID, "err", err)
				}
			}
		}
		go queue.Run(ctx)
		poller := &poll.Poller{
			Lister: lister, Queue: queue, Store: st, Buffer: st, Signal: signal.C(), MaxAge: tun.PollLookback,
			Interval: func() time.Duration {
				set, err := st.Settings(ctx)
				if err != nil {
					return 0
				}
				return set.PollInterval
			},
		}
		go poller.Run(ctx)

		var ingestor *ingest.Ingestor
		if !demoMode { // demo's Source is fake data with no history worth storing
			ingestor = &ingest.Ingestor{Source: source, Store: st, SourceName: sourceName, Latest: st.LatestSampleTime}
			go ingestor.Run(ctx)
		}

		if !demoMode && tun.CanaryInterval > 0 { // demo has no real Strava session to dry-run against
			go (&canary.Runner{Inspector: inspector, Store: st, Interval: func() time.Duration { return tun.CanaryInterval }}).Run(ctx)
		}

		go func() { // apply the retention setting at start and every few hours
			for {
				if n, err := st.Prune(ctx, time.Now()); err != nil {
					slog.Error("retention", "err", err)
				} else if n.Samples > 0 || n.Events > 0 {
					slog.Info("retention deleted", "readings", n.Samples, "events", n.Events)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(6 * time.Hour):
				}
			}
		}()

		weekly := &digest.Weekly{
			Store: st, Loc: func() *time.Location { return time.Local },
			Enabled: func() bool { cfg, err := st.LoadConfig(); return err == nil && cfg.MailWeekly },
			Unit: func() render.Unit {
				if set, err := st.Settings(ctx); err == nil {
					return set.Unit
				}
				return render.MgDL
			},
			Send: func(ctx context.Context, m notify.Message) error { return sendSummary(ctx, st, vault, demoMode, m) },
			Tr:   installTr(st),
		}
		go weekly.Run(ctx)

		health := &digest.Health{
			Store: st, Loc: func() *time.Location { return time.Local }, Build: buildID,
			Enabled: func() bool { cfg, err := st.LoadConfig(); return err == nil && cfg.MailHealth },
			Send:    func(ctx context.Context, m notify.Message) error { return sendSummary(ctx, st, vault, demoMode, m) },
			Tr:      installTr(st),
		}
		go health.Run(ctx)

		if !demoMode { // the demo source is not real data
			go (&gap.Monitor{
				Latest: st.LatestSampleTime, Record: st.RecordEvent, Loc: func() *time.Location { return time.Local },
				Threshold: func() time.Duration {
					cfg, err := st.LoadConfig()
					if err != nil {
						return 0
					}
					return time.Duration(cfg.GapAlertHours) * time.Hour
				},
			}).Run(ctx)
		}

		d := &notify.Dispatcher{Outbox: st, Cooldown: tun.NotifyCooldown, MaxAge: tun.NotifyMaxAge, Link: publicLink(st), Tr: installTr(st), Loc: func() *time.Location { return time.Local }, Channels: func() []notify.Channel { return channels(st, vault, demoMode) }}
		go d.Run(ctx, 30*time.Second)

		e.Router.GET("/health", func(re *core.RequestEvent) error {
			return re.JSON(http.StatusOK, map[string]string{"status": "ok", "build": buildID})
		})

		// glucoseWindow is how far back the /metrics glucose gauges look: long
		// enough for TIR to mean something, short enough to still read as
		// "now" rather than a whole day's history.
		const glucoseWindow = 3 * time.Hour
		metrics.RegisterGlucoseCollector(func(ctx context.Context) (stats.Summary, bool) {
			samples, err := st.LoadSamplesAny(ctx, time.Now().Add(-glucoseWindow), time.Now())
			if err != nil || len(samples) == 0 {
				return stats.Summary{}, false
			}
			set, err := st.Settings(ctx)
			rng := stats.DefaultRange
			if err == nil {
				rng = set.Range
			}
			return stats.Summarize(samples, rng)
		})
		e.Router.GET("/metrics", apis.WrapStdHandler(metrics.RequireToken(toks.Verify, metrics.Handler())))

		h := &trigger.Handler{
			Tokens: toks, Signal: signal, Events: st,
			ClientIP: proxies.IP,
		}
		// Any method reaches the handler, which answers non-POST with 405.
		e.Router.Any("/api/trigger", apis.WrapStdHandler(h))

		// GET /export/tasker.prf.xml?token=... generates a ready-to-import
		// Tasker profile for this deployment. Auth is the token itself, same
		// trust model as /api/trigger: it has to be reachable from the
		// phone's own browser or camera app, not a logged-in session.
		e.Router.GET("/export/tasker.prf.xml", apis.WrapStdHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.URL.Query().Get("token")
			ok, verr := toks.Verify(token)
			if verr != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "invalid or missing token", http.StatusUnauthorized)
				return
			}
			scheme := "http"
			if proxies.Secure(r) {
				scheme = "https"
			}
			body := taskerprofile.Build(scheme+"://"+r.Host, token)
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="glucava-tasker.prf.xml"`)
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(body)
		})))

		ui := &web.Server{
			App: app, Store: st, Vault: vault, Tokens: toks, Jobs: queue, Signal: signal, Bus: changes, Progress: progress,
			Logs:    logHandler,
			Proxies: proxies, Session: session, SourceName: sourceName, Build: buildID, Demo: demoMode,
			SendTest: func(ctx context.Context) error {
				return sendTest(ctx, channels(st, vault, demoMode), installTr(st).Get())
			},
			StravaLogin: stravaLogin,
			GlucoseTest: func(ctx context.Context) error {
				_, err := source.Samples(ctx, time.Now().Add(-10*time.Minute), time.Now())
				if errors.Is(err, glucose.ErrTooOld) {
					return nil
				}
				return err
			},
			Poll: poller.Once,
			Resync: func(ctx context.Context) (int, error) {
				if ingestor == nil {
					return 0, nil
				}
				return ingestor.ForceOnce(ctx)
			},
			FindActivity: findActivity,
			Restore:      proc.Restore,
			LatestGlucose: func(ctx context.Context) (*stats.Sample, error) {
				s, err := source.Samples(ctx, time.Now().Add(-30*time.Minute), time.Now())
				if errors.Is(err, glucose.ErrTooOld) {
					return nil, nil
				}
				if err != nil {
					return nil, err
				}
				if len(s) == 0 {
					return nil, nil
				}
				last := s[len(s)-1]
				return &last, nil
			},
		}
		uiHandler := apis.WrapStdHandler(metrics.InstrumentHandler(ui.Handler()))
		for _, pattern := range web.Routes {
			e.Router.GET(pattern, uiHandler)
			e.Router.POST(pattern, uiHandler)
		}

		return e.Next()
	})

	if err := app.Start(); err != nil {
		slog.Error("start", "err", err)
		os.Exit(1)
	}
}

// demoDefaults sets a login for demo mode unless one is configured.
func demoDefaults() {
	if os.Getenv("GLUCAVA_ADMIN_EMAIL") == "" {
		_ = os.Setenv("GLUCAVA_ADMIN_EMAIL", "demo@example.test")
		pw := os.Getenv("GLUCAVA_ADMIN_PASSWORD")
		if pw == "" {
			pw = security.RandomString(20)
			_ = os.Setenv("GLUCAVA_ADMIN_PASSWORD", pw)
		}
		slog.Info("demo mode: sign in with demo@example.test", "password", pw)
	}
}

// storeDemoCookies stores harmless placeholder cookies so the Strava page shows a session.
func storeDemoCookies(vault *secrets.Vault) error {
	if _, ok, _ := vault.Get(secrets.NameStravaCookies); ok {
		return nil
	}
	enc, err := strava.EncodeCookies([]strava.Cookie{
		{Name: "_strava4_session", Value: "demo", Domain: ".strava.com", Path: "/", HTTPOnly: true, Secure: true},
		{Name: "sp", Value: "demo", Domain: ".strava.com", Path: "/", Expires: time.Now().Add(180 * 24 * time.Hour)},
	})
	if err != nil {
		return err
	}
	return vault.Set(secrets.NameStravaCookies, enc)
}

// sendTest delivers a test message to every configured channel.
func sendTest(ctx context.Context, chans []notify.Channel, tr *i18n.Translator) error {
	if len(chans) == 0 {
		return errors.New("no channel is set up; save a ntfy or webhook URL, an email recipient (with SMTP configured), or enable push on a device, first")
	}
	msg := notify.Message{
		Type: notify.TypeTest, Severity: "info", Title: notify.Title(tr, notify.TypeTest),
		Body: tr.T("notify.test.body"), Time: time.Now(),
	}
	var errs []error
	for _, c := range chans {
		if err := c.Send(ctx, msg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// channels builds the notification channels from the current settings.
func channels(st *store.PB, vault *secrets.Vault, demo bool) []notify.Channel {
	cfg, err := st.LoadConfig()
	if err != nil {
		slog.Error("notify read settings", "err", err)
		return nil
	}
	var out []notify.Channel
	if cfg.NtfyURL != "" {
		tok, _, _ := vault.Get(secrets.NameNtfyToken)
		out = append(out, &notify.Ntfy{URL: cfg.NtfyURL, Token: tok})
	}
	if cfg.WebhookURL != "" {
		secret, _, _ := vault.Get(secrets.NameWebhookSecret)
		out = append(out, &notify.Webhook{URL: cfg.WebhookURL, Secret: secret})
	}
	if e := newEmail(cfg, vault, func(t string) bool { return t == notify.TypeTest || (notify.IsAlert(t) && cfg.MailAlerts) }, installTr(st)); e != nil {
		out = append(out, e)
	}
	wants := func(t string) bool { return t == notify.TypeTest || (notify.IsAlert(t) && cfg.PushAlerts) }
	if p := newPush(st, vault, cfg, demo, wants); p != nil {
		out = append(out, p)
	}
	return out
}

// newPush builds the Web Push channel, or returns nil while no device is
// subscribed (so events stay pending instead of counting as delivered).
func newPush(st *store.PB, vault *secrets.Vault, cfg store.Config, demo bool, wants func(msgType string) bool) *notify.WebPush {
	subs, err := st.PushSubscriptions(context.Background())
	if err != nil || len(subs) == 0 {
		return nil
	}
	p, err := notify.NewWebPush(st, vault, cfg.PublicURL, cfg.EmailTo, demo)
	if err != nil {
		slog.Error("notify: web push setup", "err", err)
		return nil
	}
	p.Wants = wants
	return p
}

// newEmail builds the email channel from settings, or returns nil when email
// is not set up. wants picks which kinds of message it sends.
func newEmail(cfg store.Config, vault *secrets.Vault, wants func(msgType string) bool, tr notify.Translator) *notify.Email {
	if cfg.EmailTo == "" || cfg.SMTPHost == "" || cfg.SMTPSender == "" {
		return nil
	}
	pw, _, _ := vault.Get(secrets.NameSMTPPassword)
	client := &mailer.SMTPClient{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: pw, TLS: cfg.SMTPTLS}
	return &notify.Email{
		SendFunc: client.Send,
		From:     mail.Address{Name: cfg.SMTPSenderName, Address: cfg.SMTPSender},
		To:       cfg.EmailTo,
		Wants:    wants,
		Tr:       tr,
	}
}

// installTr follows the installation language setting at call time. Background
// texts (alerts, summaries, the test message) have no request to take a
// language from, so "auto" means English there.
func installTr(st *store.PB) notify.Translator {
	return func() *i18n.Translator {
		cfg, err := st.LoadConfig()
		if err != nil {
			return i18n.English()
		}
		return i18n.Default().Match("", cfg.Language)
	}
}

// sendSummary sends a summary message to email and to push devices, each if
// that kind is switched on for it. ntfy and webhooks carry alerts only.
func sendSummary(ctx context.Context, st *store.PB, vault *secrets.Vault, demo bool, m notify.Message) error {
	cfg, err := st.LoadConfig()
	if err != nil {
		return err
	}
	isType := func(t string) bool { return m.Type == t }
	mailOn := (isType(notify.TypeActivitySummary) && cfg.MailActivity) || (isType(notify.TypeWeeklySummary) && cfg.MailWeekly) ||
		(isType(notify.TypeHealthReport) && cfg.MailHealth)
	pushOn := cfg.PushSummaries && (isType(notify.TypeActivitySummary) || isType(notify.TypeWeeklySummary) || isType(notify.TypeHealthReport))
	m.Link, m.LinkLabel = notify.LinkFor(installTr(st).Get(), cfg.PublicURL, m)
	var errs []error
	if e := newEmail(cfg, vault, nil, installTr(st)); mailOn && e != nil {
		if err := e.Send(ctx, m); err != nil {
			errs = append(errs, err)
		}
	}
	if p := newPush(st, vault, cfg, demo, nil); pushOn && p != nil {
		if err := p.Send(ctx, m); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// publicLink adds a web UI link to alert messages when a public URL is set.
func publicLink(st *store.PB) func(notify.Translator, notify.Message) (string, string) {
	return func(tr notify.Translator, m notify.Message) (string, string) {
		cfg, err := st.LoadConfig()
		if err != nil {
			return "", ""
		}
		return notify.LinkFor(tr.Get(), cfg.PublicURL, m)
	}
}

// tokenCommand adds "glucava token create|list|revoke".
func tokenCommand(app core.App, m *tokens.Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use: "token", Short: "Manage trigger API tokens",
		// Migrations otherwise run only on "serve", so a fresh data dir has no tables yet.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return app.RunAppMigrations() },
	}
	cmd.AddCommand(
		&cobra.Command{
			Use: "create <name>", Short: "Create a token and print it once", Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				tok, err := m.Create(args[0])
				if err != nil {
					return err
				}
				fmt.Println(tok)
				return nil
			},
		},
		&cobra.Command{
			Use: "list", Short: "List tokens", Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				list, err := m.List()
				if err != nil {
					return err
				}
				for _, t := range list {
					state, used := "active", "never"
					if t.Revoked {
						state = "revoked"
					}
					if !t.LastUsed.IsZero() {
						used = t.LastUsed.Format(time.RFC3339)
					}
					fmt.Printf("%s\t%s\tlast used: %s\n", t.Name, state, used)
				}
				return nil
			},
		},
		&cobra.Command{
			Use: "revoke <name>", Short: "Revoke a token", Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				if err := m.Revoke(args[0]); err != nil {
					return err
				}
				fmt.Printf("revoked %s\n", args[0])
				return nil
			},
		},
	)
	return cmd
}

// blockPocketBase hides PocketBase's own admin UI and REST API. Glucava serves
// its own pages and only /api/trigger; set GLUCAVA_ADMIN_UI=1 to expose the rest.
func blockPocketBase(e *core.RequestEvent) error {
	p := e.Request.URL.Path
	if p == "/api/trigger" {
		return e.Next()
	}
	if p == "/_" || strings.HasPrefix(p, "/_/") || p == "/api" || strings.HasPrefix(p, "/api/") {
		return e.NotFoundError("", nil)
	}
	return e.Next()
}
