package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"github.com/MrCodeEU/glucava/internal/glucose/importers"
	"github.com/MrCodeEU/glucava/internal/store"
)

// glucoseCommand adds "glucava glucose import", a one-shot bulk import of a
// CGM export file (Libre, Nightscout, ...) into the local glucose_samples
// table, for backfilling history no live Source has any more. See
// internal/glucose/importers for the format list and how to add one.
func glucoseCommand(app core.App, st *store.PB) *cobra.Command {
	cmd := &cobra.Command{
		Use: "glucose", Short: "Import glucose readings from a file",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return app.RunAppMigrations() },
	}

	var format, source string
	imp := &cobra.Command{
		Use:   "import <file>",
		Short: "Import readings from an export file (see --format)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			importer, ok := importers.Get(format)
			if !ok {
				return fmt.Errorf("unknown --format %q; known formats: %s", format, strings.Join(importers.Names(), ", "))
			}
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()

			if err := importers.CheckZipSupport(importer, f, format); err != nil {
				return err
			}
			samples, skipped, err := importer.Parse(f)
			if err != nil {
				return fmt.Errorf("parse %s as %s: %w", args[0], format, err)
			}
			if len(samples) == 0 {
				return fmt.Errorf("no readings found in %s; wrong --format, or an empty/unexpected export", args[0])
			}
			if err := st.SaveSamples(cmd.Context(), source, samples); err != nil {
				return fmt.Errorf("store samples: %w", err)
			}
			plural := "s"
			if len(samples) == 1 {
				plural = ""
			}
			fmt.Printf("stored %d reading%s as source %q", len(samples), plural, source)
			if skipped > 0 {
				fmt.Printf(" (%d rows skipped: not a glucose reading, or unparseable)", skipped)
			}
			fmt.Println()
			return nil
		},
	}
	imp.Flags().StringVar(&format, "format", "", "export format: "+strings.Join(importers.Names(), ", ")+" (required)")
	imp.Flags().StringVar(&source, "source", "import", `label stored with these readings, e.g. "libre"; distinguishes them from the live source`)
	_ = imp.MarkFlagRequired("format")

	cmd.AddCommand(imp)
	return cmd
}
