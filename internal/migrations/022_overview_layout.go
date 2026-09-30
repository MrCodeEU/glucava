package migrations

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

func init() {
	core.AppMigrations.Register(up022, nil, "022_overview_layout.go")
}

// overviewLayoutOrder is the default card order at the time of this
// migration. It is frozen here on purpose: a migration must keep producing
// the same result even after the card registry grows (a card added later is
// appended by store.NormalizeOverviewLayout when the layout is read).
var overviewLayoutOrder = []string{
	"kpis", "tir", "agp", "trend", "heatmap", "calendar", "dayparts",
	"episodes", "by_sport", "insights", "table", "sources",
}

// layoutFromToggles turns the five old overview_show_* booleans into a
// layout. The general-summary toggle governs the key-numbers card and the
// source-health toggle the sources card; the cards this release adds have no
// old toggle and start enabled, so new content shows up rather than needing
// to be found.
func layoutFromToggles(trend, bySport, table, general, sourceHealth bool) string {
	enabled := map[string]bool{
		"trend": trend, "by_sport": bySport, "table": table, "kpis": general, "sources": sourceHealth,
	}
	type card struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	cards := make([]card, 0, len(overviewLayoutOrder))
	for _, id := range overviewLayoutOrder {
		on, known := enabled[id]
		if !known {
			on = true
		}
		cards = append(cards, card{ID: id, Enabled: on})
	}
	b, _ := json.Marshal(cards)
	return string(b)
}

// up022 replaces the five overview_show_* booleans with one overview_layout
// JSON setting (ordered cards with enabled flag and options) and adds the
// default range preset. The old columns stay in place, unused: dropping a
// column cannot be undone.
func up022(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(
		&core.TextField{Name: "overview_layout"},
		&core.TextField{Name: "overview_default_range"},
	)
	if err := app.Save(settings); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("settings")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("overview_layout", layoutFromToggles(
			r.GetBool("overview_show_trend"), r.GetBool("overview_show_by_sport"), r.GetBool("overview_show_table"),
			r.GetBool("overview_show_general"), r.GetBool("overview_show_source_health"),
		))
		r.Set("overview_default_range", "30d")
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
