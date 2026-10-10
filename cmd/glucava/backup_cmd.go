package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/MrCodeEU/glucava/internal/backup"
)

// backupCommand is "glucava backup <file>": a consistent backup of the
// database that is safe while the server runs, for cron jobs and scripts.
func backupCommand(app core.App) *cobra.Command {
	var includeKey bool
	cmd := &cobra.Command{
		Use:   "backup [file|-]",
		Short: "Write a backup zip (works while the server runs); - writes to stdout",
		Long: `Writes one zip with a consistent copy of the database. Stored secrets stay
encrypted inside it and the encryption key is left out, so keep the key
(GLUCAVA_SECRET_KEY or secret.key) somewhere else. --include-key adds the key
file from the data dir, for a single self-contained backup you store carefully.
Without a file name it writes glucava-backup-<time>.zip to the current directory.
Restore with "glucava restore <file>" while the server is stopped.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && args[0] == "-" {
				return backup.Create(cmd.Context(), app, cmd.OutOrStdout(), buildID, includeKey)
			}
			name := "glucava-backup-" + time.Now().Format("20060102-150405") + ".zip"
			if len(args) == 1 {
				name = args[0]
			}
			if err := writeBackupFile(cmd, app, name, includeKey); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "backup written to", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&includeKey, "include-key", false, "also put the encryption key file into the backup")
	return cmd
}

// writeBackupFile writes to a temporary file next to name and renames it, so a
// failed backup never leaves a half-written file under the real name.
func writeBackupFile(cmd *cobra.Command, app core.App, name string, includeKey bool) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), ".glucava-backup-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := backup.Create(cmd.Context(), app, tmp, buildID, includeKey); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}

// runRestore runs "glucava restore" without starting PocketBase, which would
// create an empty database in the data dir first.
func runRestore() {
	var dir string
	var force bool
	root := &cobra.Command{Use: "glucava", SilenceUsage: true, SilenceErrors: true}
	cmd := &cobra.Command{
		Use:   "restore <backup.zip>",
		Short: "Restore a backup into the data dir (server must be stopped)",
		Long: `Unpacks a backup made by "glucava backup" into the data dir. Stop the server
first. An existing database is not touched unless --force is given; then it is
kept as data.db.before-restore. If the backup holds the key file it is restored
as well (an existing key file is kept unless --force). Start the same or a newer
version afterwards; migrations run on start. If the key was not in the backup,
provide it again as GLUCAVA_SECRET_KEY or secret.key.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := backup.Restore(args[0], dir, force)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "restored backup from %s (glucava %s) into %s\n", m.Created.Format(time.RFC3339), m.Build, dir)
			if !m.IncludesKey {
				fmt.Fprintln(cmd.OutOrStdout(), "the backup has no encryption key: provide the original key as GLUCAVA_SECRET_KEY or secret.key")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "pb_data", "data directory (/data in the Docker image)")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing database (kept as data.db.before-restore)")
	root.AddCommand(cmd)
	root.SetArgs(os.Args[1:])
	root.SetOut(io.Writer(os.Stdout))
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
