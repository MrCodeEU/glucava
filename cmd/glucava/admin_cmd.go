package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/store"
)

const minPasswordLen = 12

// userCommand adds "glucava user set-password <email>". The password comes from
// stdin so it stays out of the process list and shell history. Changing it also
// invalidates every existing login.
func userCommand(app core.App) *cobra.Command {
	cmd := &cobra.Command{
		Use: "user", Short: "Manage the web UI account",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return app.RunAppMigrations() },
	}
	cmd.AddCommand(&cobra.Command{
		Use: "set-password <email>", Short: "Set a new password (read from stdin) and sign out all sessions",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pass, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			pass = strings.TrimRight(pass, "\r\n")
			if pass == "" && err != nil {
				return fmt.Errorf("read password from stdin: %w", err)
			}
			if len(pass) < minPasswordLen {
				return fmt.Errorf("password must be at least %d characters", minPasswordLen)
			}
			rec, err := app.FindAuthRecordByEmail("users", args[0])
			if err != nil {
				return errors.New("no user with that email")
			}
			rec.SetPassword(pass)
			rec.RefreshTokenKey() // old cookies stop working
			if err := app.Save(rec); err != nil {
				return err
			}
			fmt.Println("password changed; all sessions signed out")
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
