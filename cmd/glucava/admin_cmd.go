package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/store"
)

const minPasswordLen = 12

// theUser finds the account to change. With no email given and exactly one
// user, that user is meant: glucava has a single web UI account, so a typo in
// the seeded email should not lock you out of fixing it.
func theUser(app core.App, email string) (*core.Record, error) {
	if email != "" {
		rec, err := app.FindAuthRecordByEmail("users", email)
		if err != nil {
			return nil, errors.New("no user with that email (run \"glucava user show\" to see the account)")
		}
		return rec, nil
	}
	all, err := app.FindAllRecords("users")
	if err != nil {
		return nil, err
	}
	switch len(all) {
	case 0:
		return nil, errors.New("there is no user yet")
	case 1:
		return all[0], nil
	}
	return nil, errors.New("more than one user exists; give the email")
}

func readPassword(cmd *cobra.Command) (string, error) {
	pass, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	pass = strings.TrimRight(pass, "\r\n")
	if pass == "" && err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	if len(pass) < minPasswordLen {
		return "", fmt.Errorf("password must be at least %d characters", minPasswordLen)
	}
	return pass, nil
}

// userCommand adds "glucava user show|set-email|set-password". The password
// comes from stdin so it stays out of the process list and shell history.
// Changing the email or password also invalidates every existing login.
func userCommand(app core.App) *cobra.Command {
	cmd := &cobra.Command{
		Use: "user", Short: "Manage the web UI account",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return app.RunAppMigrations() },
	}
	cmd.AddCommand(&cobra.Command{
		Use: "show", Short: "Print the email of the web UI account",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			all, err := app.FindAllRecords("users")
			if err != nil {
				return err
			}
			for _, rec := range all {
				fmt.Fprintln(cmd.OutOrStdout(), rec.Email())
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "set-email <new email> [current email]", Short: "Change the account email and sign out all sessions",
		Long: "The current email may be left out while there is exactly one user.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := mail.ParseAddress(args[0])
			if err != nil || a.Address != args[0] {
				return errors.New("that is not a valid email address")
			}
			cur := ""
			if len(args) == 2 {
				cur = args[1]
			}
			rec, err := theUser(app, cur)
			if err != nil {
				return err
			}
			rec.SetEmail(args[0])
			rec.RefreshTokenKey()
			if err := app.Save(rec); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "email changed; all sessions signed out")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "set-password [email]", Short: "Set a new password (read from stdin) and sign out all sessions",
		Long: "The email may be left out while there is exactly one user.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pass, err := readPassword(cmd)
			if err != nil {
				return err
			}
			email := ""
			if len(args) == 1 {
				email = args[0]
			}
			rec, err := theUser(app, email)
			if err != nil {
				return err
			}
			rec.SetPassword(pass)
			rec.RefreshTokenKey() // old cookies stop working
			if err := app.Save(rec); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "password changed; all sessions signed out")
			return nil
		},
	})
	return cmd
}

// secretsCommand adds "glucava secrets set|status|rotate-key".
func secretsCommand(app core.App) *cobra.Command {
	cmd := &cobra.Command{
		Use: "secrets", Short: "Manage stored secrets",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return app.RunAppMigrations() },
	}
	cmd.AddCommand(secretsSetCommand(app), secretsStatusCommand(app))
	cmd.AddCommand(&cobra.Command{
		Use: "rotate-key", Short: "Re-encrypt all stored secrets with a new random key",
		Long: `Stop the server first. With GLUCAVA_SECRET_KEY set, the new key is printed and you
must put it in the environment before the next start. Otherwise <data dir>/secret.key
is replaced. Back up the data dir first.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			vault, err := openVault(app)
			if err != nil {
				return err
			}
			key, err := secrets.NewKey()
			if err != nil {
				return err
			}
			next, err := secrets.NewCipher(key)
			if err != nil {
				return err
			}
			enc := secrets.EncodeKey(key)

			// Persist the new key before the database changes, so a crash cannot
			// leave secrets sealed with a key that exists nowhere.
			var pending string
			if secrets.KeyFromEnv() {
				fmt.Fprintf(os.Stderr, "new key (set GLUCAVA_SECRET_KEY to this):\n%s\n", enc)
			} else {
				pending = secrets.KeyFilePath(app.DataDir()) + ".new"
				if err := os.WriteFile(pending, []byte(enc), 0o600); err != nil {
					return err
				}
			}
			if err := vault.Reencrypt(next); err != nil {
				if pending != "" {
					_ = os.Remove(pending)
				}
				return err
			}
			if pending != "" {
				if err := os.Rename(pending, secrets.KeyFilePath(app.DataDir())); err != nil {
					return fmt.Errorf("secrets are re-encrypted but the key file was not replaced; move %s to secret.key: %w", pending, err)
				}
			}
			fmt.Println("secrets re-encrypted with the new key")
			return nil
		},
	})
	return cmd
}

// dataCommand adds "glucava data purge --yes".
func dataCommand(app core.App, st *store.PB) *cobra.Command {
	cmd := &cobra.Command{
		Use: "data", Short: "Manage stored glucose data",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return app.RunAppMigrations() },
	}
	var yes bool
	purge := &cobra.Command{
		Use: "purge", Short: "Delete all readings, activities and events (settings, credentials and tokens stay)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes {
				return errors.New("this deletes all stored data; repeat with --yes")
			}
			n, err := st.PurgeAll(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("deleted %d readings, %d activities, %d events\n", n.Samples, n.Activities, n.Events)
			return nil
		},
	}
	purge.Flags().BoolVar(&yes, "yes", false, "confirm the deletion")
	cmd.AddCommand(purge)
	return cmd
}
