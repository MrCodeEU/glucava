package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/store"
)

// scriptableSecrets are the vault entries "secrets set" may write. Strava
// cookies have their own command (strava cookies import).
var scriptableSecrets = []string{
	secrets.NameDexcomPassword, secrets.NameNtfyToken, secrets.NameWebhookSecret, secrets.NameSMTPPassword,
}

// configCommand adds "glucava config list|get|set|apply": every setting the
// web UI edits, through the same validation, for scripted setups.
func configCommand(app core.App, st *store.PB) *cobra.Command {
	cmd := &cobra.Command{
		Use: "config", Short: "Read and change settings (same rules as the web UI)",
		Long: `Settings can be read and changed while the server runs or stopped.
Secrets are not settings: use "glucava secrets set <name>".
GLUCAVA_RETENTION_DAYS, if set, overrides retention_days at every server start.`,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return app.RunAppMigrations() },
		SilenceUsage:      true, // a validation error is not a usage error
	}
	cmd.AddCommand(
		&cobra.Command{
			Use: "list", Short: "Print every setting as key=value", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, err := st.LoadConfig()
				if err != nil {
					return err
				}
				for _, k := range store.ConfigKeys() {
					v, _ := cfg.Get(k)
					fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n", k, v)
				}
				return nil
			},
		},
		&cobra.Command{
			Use: "get <key>", Short: "Print one setting", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := st.LoadConfig()
				if err != nil {
					return err
				}
				v, err := cfg.Get(args[0])
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), v)
				return nil
			},
		},
		&cobra.Command{
			Use: "set key=value [key=value ...]", Short: "Change settings; all values are validated together", Args: cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				pairs, err := parsePairs(args)
				if err != nil {
					return err
				}
				return applyConfig(cmd.OutOrStdout(), st, pairs)
			},
		},
		&cobra.Command{
			Use: "apply <file|->", Short: "Apply key=value lines from a file (# comments allowed); idempotent", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				r := cmd.InOrStdin()
				if args[0] != "-" {
					f, err := os.Open(args[0])
					if err != nil {
						return err
					}
					defer func() { _ = f.Close() }()
					r = f
				}
				var lines []string
				sc := bufio.NewScanner(r)
				for sc.Scan() {
					line := strings.TrimSpace(sc.Text())
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					lines = append(lines, line)
				}
				if err := sc.Err(); err != nil {
					return err
				}
				pairs, err := parsePairs(lines)
				if err != nil {
					return err
				}
				return applyConfig(cmd.OutOrStdout(), st, pairs)
			},
		},
	)
	return cmd
}

type pair struct{ key, value string }

func parsePairs(args []string) ([]pair, error) {
	var out []pair
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("%q is not key=value", a)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out = append(out, pair{k, v})
	}
	return out, nil
}

// applyConfig sets every pair on the stored config, validates the result as a
// whole (related keys such as smtp_host and smtp_sender_address may arrive in
// any order), then saves only if something changed. Nothing is saved on error.
func applyConfig(out io.Writer, st *store.PB, pairs []pair) error {
	cfg, err := st.LoadConfig()
	if err != nil {
		return err
	}
	var changed []string
	for _, p := range pairs {
		before, err := cfg.Get(p.key)
		if err != nil {
			return err
		}
		if err := cfg.Set(p.key, p.value); err != nil {
			return err
		}
		if after, _ := cfg.Get(p.key); after != before {
			changed = append(changed, fmt.Sprintf("%s: %s -> %s", p.key, before, after))
		}
	}
	if msg := cfg.Validate(); msg != "" {
		return errors.New(msg)
	}
	if len(changed) == 0 {
		fmt.Fprintln(out, "unchanged")
		return nil
	}
	if err := st.SaveConfig(cfg); err != nil {
		return err
	}
	sort.Strings(changed)
	for _, c := range changed {
		fmt.Fprintln(out, "changed", c)
	}
	return nil
}

// secretsSetCommand is "glucava secrets set <name>": the value comes from
// stdin, never argv, so it stays out of the process list and shell history.
func secretsSetCommand(app core.App) *cobra.Command {
	return &cobra.Command{
		Use:   "set <name>",
		Short: "Store a secret read from stdin (" + strings.Join(scriptableSecrets, ", ") + ")",
		Args:  cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !knownSecret(args[0]) {
				return fmt.Errorf("unknown secret %q (known: %s)", args[0], strings.Join(scriptableSecrets, ", "))
			}
			val, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			val = strings.TrimRight(val, "\r\n")
			if val == "" {
				if err != nil && !errors.Is(err, io.EOF) {
					return fmt.Errorf("read value from stdin: %w", err)
				}
				return errors.New("empty value")
			}
			vault, err := openVault(app)
			if err != nil {
				return err
			}
			if err := vault.Set(args[0], val); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stored %s\n", args[0])
			return nil
		},
	}
}

// secretsStatusCommand is "glucava secrets status": which secrets are stored,
// never their values.
func secretsStatusCommand(app core.App) *cobra.Command {
	return &cobra.Command{
		Use: "status", Short: "Show which secrets are stored (never the values)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vault, err := openVault(app)
			if err != nil {
				return err
			}
			for _, n := range append([]string{secrets.NameStravaCookies}, scriptableSecrets...) {
				_, ok, err := vault.Get(n)
				if err != nil {
					return err
				}
				state := "unset"
				if ok {
					state = "set"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n", n, state)
			}
			return nil
		},
	}
}

func knownSecret(name string) bool {
	for _, n := range scriptableSecrets {
		if n == name {
			return true
		}
	}
	return false
}
