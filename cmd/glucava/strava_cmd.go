package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/strava"
)

// stravaCommand adds "glucava strava cookies|check|list". None of them change
// anything on Strava, and none print cookie values.
func stravaCommand(app core.App) *cobra.Command {
	cmd := &cobra.Command{
		Use: "strava", Short: "Set up and dry-run the Strava browser session",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return app.RunAppMigrations() },
	}

	cookies := &cobra.Command{Use: "cookies", Short: "Manage the stored Strava session cookies"}
	cookies.AddCommand(
		&cobra.Command{
			Use:   "import [file]",
			Short: "Store cookies from a file or stdin (JSON export, cookies.txt or a Cookie header)",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				var raw []byte
				var err error
				if len(args) == 1 {
					raw, err = os.ReadFile(args[0])
				} else {
					raw, err = io.ReadAll(cmd.InOrStdin())
				}
				if err != nil {
					return err
				}
				list, err := strava.ParseCookies(string(raw))
				if err != nil {
					return err
				}
				enc, err := strava.EncodeCookies(list)
				if err != nil {
					return err
				}
				vault, err := openVault(app)
				if err != nil {
					return err
				}
				if err := vault.Set(secrets.NameStravaCookies, enc); err != nil {
					return err
				}
				fmt.Printf("stored %d cookies\n", len(list))
				if !strava.HasSession(list) {
					fmt.Println("warning: no _strava4_session cookie found; the session will probably be rejected")
				}
				return nil
			},
		},
		&cobra.Command{
			Use: "status", Short: "Show the stored cookie names and expiry (never the values)", Args: cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				vault, err := openVault(app)
				if err != nil {
					return err
				}
				raw, ok, err := vault.Get(secrets.NameStravaCookies)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("no cookies stored")
					return nil
				}
				list, err := strava.DecodeCookies(raw)
				if err != nil {
					return err
				}
				fmt.Printf("%d cookies, session cookie present: %v\n", len(list), strava.HasSession(list))
				for _, c := range list {
					exp := "session"
					if !c.Expires.IsZero() {
						exp = c.Expires.Format(time.RFC3339)
					}
					fmt.Printf("  %s\texpires: %s\n", c.Name, exp)
				}
				return nil
			},
		},
	)

	var htmlFile string
	var fullDesc bool
	var probePhoto bool
	var probeShot string
	check := &cobra.Command{
		Use:   "check [activity-id]",
		Short: "Dry run: test the session, or inspect an activity's edit page without saving",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := writerFromVault(app)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if len(args) == 0 {
				if err := w.CheckSession(ctx); err != nil {
					return err
				}
				fmt.Println("session OK: Strava accepts the stored cookies")
				return nil
			}
			if probePhoto {
				return runPhotoProbe(ctx, w, args[0], probeShot)
			}
			rep, err := w.Inspect(ctx, args[0], htmlFile != "")
			if err != nil {
				return err
			}
			fmt.Print(rep)
			if fullDesc {
				fmt.Printf("\nfull description (%d bytes):\n%q\n", len(rep.Description), rep.Description)
			}
			if htmlFile != "" && rep.HTML != "" {
				if err := os.WriteFile(htmlFile, []byte(rep.HTML), 0o600); err != nil {
					return err
				}
				fmt.Printf("\npage source written to %s (contains your activity data)\n", htmlFile)
			}
			if rep.LoggedIn && rep.DescriptionSelector == "" {
				return fmt.Errorf("description field not found")
			}
			return nil
		},
	}
	check.Flags().StringVar(&htmlFile, "html", "", "write the edit page source to this file")
	check.Flags().BoolVar(&probePhoto, "photo", false, "dry run of the chart photo upload: choose a small test image in the uploader, report what the page does, never save (the uploader may still send the file to Strava)")
	check.Flags().StringVar(&probeShot, "screenshot", "", "with --photo: write a screenshot of the page after the upload attempt to this file")
	check.Flags().BoolVar(&fullDesc, "full", false, "print the whole description text, quoted, not just its length")

	var raw bool
	list := &cobra.Command{
		Use: "list", Short: "Dry run: list recent activities as the poller sees them", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w, err := writerFromVault(app)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			body, err := w.FetchRecentRaw(ctx, 20)
			if err != nil {
				return err
			}
			if raw {
				var pretty any
				if json.Unmarshal(body, &pretty) == nil {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(pretty)
				}
				fmt.Println(string(body))
				return nil
			}
			acts, err := strava.ParseTrainingActivities(body, time.Local)
			if err != nil {
				return err
			}
			if len(acts) == 0 {
				fmt.Println("no activities parsed; run with --raw to see the response")
			}
			for _, a := range acts {
				fmt.Printf("%s\t%s\t%s\t%s\t%s\n", a.StravaID, a.Start.Local().Format("2006-01-02 15:04"), a.Duration, a.Sport, a.Name)
			}
			return nil
		},
	}
	list.Flags().BoolVar(&raw, "raw", false, "print the raw JSON response")

	cmd.AddCommand(cookies, check, list)
	return cmd
}

func writerFromVault(app core.App) (*strava.Writer, error) {
	vault, err := openVault(app)
	if err != nil {
		return nil, err
	}
	tun, err := loadTuning(os.Getenv) // so `strava check` sees the same selector overrides as the server
	if err != nil {
		return nil, err
	}
	return newStravaWriter(vault, tun), nil
}
