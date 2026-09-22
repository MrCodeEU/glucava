package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/spf13/cobra"

	"github.com/MrCodeEU/glucava/internal/bootstrap"
	"github.com/MrCodeEU/glucava/internal/bus"
	"github.com/MrCodeEU/glucava/internal/clientip"
	"github.com/MrCodeEU/glucava/internal/demo"
	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/jobs"
	_ "github.com/MrCodeEU/glucava/internal/migrations"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/poll"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
	"github.com/MrCodeEU/glucava/internal/strava"
	"github.com/MrCodeEU/glucava/internal/tokens"
	"github.com/MrCodeEU/glucava/internal/trigger"
	"github.com/MrCodeEU/glucava/internal/web"
)

var buildID = "dev"

func main() {
	app := pocketbase.New()
	changes := &bus.Bus{}
	toks := &tokens.Manager{App: app}
	st := &store.PB{App: app, Changed: changes.Publish}
	signal := trigger.NewSignal()
	demoMode := os.Getenv("GLUCAVA_DEMO") == "1"

	bootstrap.EnforceSingleUser(app)
	app.RootCmd.AddCommand(tokenCommand(app, toks), stravaCommand(app), dexcomCommand(app), userCommand(app), secretsCommand(app), dataCommand(app, st))

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if demoMode {
			demoDefaults()
		}
		proxies, err := clientip.Parse(os.Getenv("GLUCAVA_TRUSTED_PROXIES"))
		if err != nil {
			return err
		}
		if err := os.Chmod(app.DataDir(), 0o700); err != nil {
			log.Printf("chmod data dir: %v", err)
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
		vault, err := openVault(app)
		if err != nil {
			return err
		}
		if !secrets.KeyFromEnv() {
			log.Printf("encryption key is stored in %s; keep it out of backups of the data dir, or set GLUCAVA_SECRET_KEY", secrets.KeyFilePath(app.DataDir()))
		}
		if err := bootstrap.EnsureDexcomCredential(app, vault, secrets.NameDexcomPassword); err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(context.Background())
		app.OnTerminate().BindFunc(func(te *core.TerminateEvent) error {
			cancel()
			return te.Next()
		})

		// The pipeline uses real Strava and Dexcom, or stand-ins in demo mode.
		var (
			writer      jobs.Writer
			source      glucose.Source
			lister      poll.Lister
			session     web.SessionChecker
			sourceName  = "dexcom"
			stravaLogin func(ctx context.Context, email, password string) error
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
			sw := newStravaWriter(vault)
			writer, source, lister, session = sw, &dexcomSource{st: st, vault: vault}, sw, sw
			stravaLogin = sw.Login
		}

		proc := &jobs.Processor{Store: st, Source: source, SourceName: sourceName, Writer: writer}
		queue := jobs.NewQueue(proc, jobs.DefaultBackoff, 64)
		go queue.Run(ctx)
		poller := &poll.Poller{
			Lister: lister, Queue: queue, Store: st, Signal: signal.C(),
			Interval: func() time.Duration {
				set, err := st.Settings(ctx)
				if err != nil {
					return 0
				}
				return set.PollInterval
			},
		}
		go poller.Run(ctx)

		go func() { // apply the retention setting at start and every few hours
			for {
				if n, err := st.Prune(ctx, time.Now()); err != nil {
					log.Printf("retention: %v", err)
				} else if n.Samples > 0 || n.Events > 0 {
					log.Printf("retention: deleted %d readings and %d events", n.Samples, n.Events)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(6 * time.Hour):
				}
			}
		}()

		d := &notify.Dispatcher{Outbox: st, Channels: func() []notify.Channel { return channels(st, vault) }}
		go d.Run(ctx, 30*time.Second)

		e.Router.GET("/health", func(re *core.RequestEvent) error {
			return re.JSON(http.StatusOK, map[string]string{"status": "ok", "build": buildID})
		})

		h := &trigger.Handler{
			Tokens: toks, Signal: signal, Events: st,
			ClientIP: proxies.IP,
		}
		e.Router.POST("/api/trigger", apis.WrapStdHandler(h))

		ui := &web.Server{
			App: app, Store: st, Vault: vault, Tokens: toks, Jobs: queue, Signal: signal, Bus: changes,
			Proxies: proxies, Session: session, SourceName: sourceName, Build: buildID, Demo: demoMode,
			SendTest:    func(ctx context.Context) error { return sendTest(ctx, channels(st, vault)) },
			StravaLogin: stravaLogin,
			GlucoseTest: func(ctx context.Context) error {
				_, err := source.Samples(ctx, time.Now().Add(-10*time.Minute), time.Now())
				if errors.Is(err, glucose.ErrTooOld) {
					return nil
				}
				return err
			},
			Poll: poller.Once,
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
		uiHandler := apis.WrapStdHandler(ui.Handler())
		for _, pattern := range web.Routes {
			e.Router.GET(pattern, uiHandler)
			e.Router.POST(pattern, uiHandler)
		}

		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
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
		log.Printf("demo mode: sign in with demo@example.test / %s", pw)
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
func sendTest(ctx context.Context, chans []notify.Channel) error {
	if len(chans) == 0 {
		return errors.New("no channel is set up; save a ntfy or webhook URL first")
	}
	msg := notify.Message{
		Type: "test", Severity: "info", Title: "glucava test",
		Body: "This is a test notification from glucava.", Time: time.Now(),
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
func channels(st *store.PB, vault *secrets.Vault) []notify.Channel {
	ntfyURL, webhookURL, err := st.NotifySettings()
	if err != nil {
		log.Printf("notify: read settings: %v", err)
		return nil
	}
	var out []notify.Channel
	if ntfyURL != "" {
		tok, _, _ := vault.Get(secrets.NameNtfyToken)
		out = append(out, &notify.Ntfy{URL: ntfyURL, Token: tok})
	}
	if webhookURL != "" {
		secret, _, _ := vault.Get(secrets.NameWebhookSecret)
		out = append(out, &notify.Webhook{URL: webhookURL, Secret: secret})
	}
	return out
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
			RunE: func(_ *cobra.Command, args []string) error { return m.Revoke(args[0]) },
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
