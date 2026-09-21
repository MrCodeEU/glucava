package main

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/MrCodeEU/glucava/internal/secrets"
)

// dexcomCommand adds "glucava dexcom set <username>", which reads the password
// from stdin so it never appears in the process list or shell history.
func dexcomCommand(app core.App) *cobra.Command {
	var region string
	cmd := &cobra.Command{
		Use:   "dexcom set <username>",
		Short: "Store Dexcom Share credentials (password is read from stdin)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "set" {
				return errors.New(`usage: glucava dexcom set <username> < password-file`)
			}
			if err := app.RunAppMigrations(); err != nil {
				return err
			}
			if region != "us" && region != "ous" && region != "jp" {
				return errors.New(`--region must be "us", "ous" or "jp"`)
			}
			pass, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			pass = strings.TrimRight(pass, "\r\n")
			if pass == "" {
				if err != nil {
					return fmt.Errorf("read password from stdin: %w", err)
				}
				return errors.New("empty password")
			}

			recs, err := app.FindRecordsByFilter("settings", "", "created", 1, 0)
			if err != nil || len(recs) == 0 {
				return fmt.Errorf("settings row not found: %v", err)
			}
			recs[0].Set("dexcom_username", args[1])
			recs[0].Set("dexcom_region", region)
			if err := app.Save(recs[0]); err != nil {
				return err
			}
			vault, err := openVault(app)
			if err != nil {
				return err
			}
			if err := vault.Set(secrets.NameDexcomPassword, pass); err != nil {
				return err
			}
			fmt.Printf("stored Dexcom credentials for %s (region %s)\n", args[1], region)
			return nil
		},
	}
	cmd.Flags().StringVar(&region, "region", "ous", `Dexcom Share region: "us", "ous" (outside US) or "jp"`)
	return cmd
}
