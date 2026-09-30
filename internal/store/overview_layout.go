package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OverviewCard is one card of the Overview page in its configured place:
// the layout is an ordered list, top to bottom.
type OverviewCard struct {
	ID      string            `json:"id"`
	Enabled bool              `json:"enabled"`
	Options map[string]string `json:"options,omitempty"`
}

// OverviewCardDef describes a card the Overview page can show.
type OverviewCardDef struct {
	ID, Title, Help string
	Options         []OverviewOptionDef
}

// OverviewOptionDef is one per-card option: a small fixed choice.
type OverviewOptionDef struct {
	Key, Label string
	Choices    []OverviewChoice // the first is the default
}

// OverviewChoice is one value of an OverviewOptionDef.
type OverviewChoice struct{ Value, Label string }

// OverviewCards is every card the Overview page knows, in the default order.
// It is the single registry: the page renders from it, the settings screen
// lists it, and layout parsing drops anything not in it.
var OverviewCards = []OverviewCardDef{
	{ID: "kpis", Title: "Key numbers", Help: "Time in range, GMI, average, variability, risk index and data coverage."},
	{ID: "tir", Title: "Time in range", Help: "Five-band split over all readings, and during activities."},
	{ID: "agp", Title: "Daily profile (AGP)", Help: "Median and percentile bands by time of day.", Options: []OverviewOptionDef{
		{Key: "hours", Label: "Readings", Choices: []OverviewChoice{{"all", "All readings"}, {"rest", "Outside activities"}}},
	}},
	{ID: "trend", Title: "Daily trends", Help: "Average, range, time in range and variability day by day."},
	{ID: "heatmap", Title: "Weekday and hour", Help: "When in the week glucose runs high or low.", Options: []OverviewOptionDef{
		{Key: "metric", Label: "Colour by", Choices: []OverviewChoice{{"tir", "Time in range"}, {"mean", "Average glucose"}}},
	}},
	{ID: "calendar", Title: "Calendar", Help: "Time in range for every day."},
	{ID: "dayparts", Title: "Time of day", Help: "Night, morning, afternoon and evening compared."},
	{ID: "episodes", Title: "Lows and highs", Help: "Episodes of at least 15 minutes below or above range."},
	{ID: "by_sport", Title: "By activity type", Help: "How each kind of activity goes."},
	{ID: "insights", Title: "Activity insights", Help: "Starting glucose against change, best and worst activities."},
	{ID: "table", Title: "Activities", Help: "The activity list for the range."},
	{ID: "sources", Title: "Glucose sources", Help: "Newest reading per source, to spot a stopped connection or a failed import."},
}

// OverviewCardDefByID returns a card's definition.
func OverviewCardDefByID(id string) (OverviewCardDef, bool) {
	for _, d := range OverviewCards {
		if d.ID == id {
			return d, true
		}
	}
	return OverviewCardDef{}, false
}

// DefaultOverviewLayout is every card, enabled, in the default order.
func DefaultOverviewLayout() []OverviewCard {
	out := make([]OverviewCard, len(OverviewCards))
	for i, d := range OverviewCards {
		out[i] = OverviewCard{ID: d.ID, Enabled: true}
	}
	return out
}

// NormalizeOverviewLayout cleans a layout: unknown cards and repeats are
// dropped, a card missing from the list is appended (enabled, so a card added
// in a later version shows up rather than needing to be found), and an
// option no card defines, or with a value outside its choices, is removed.
func NormalizeOverviewLayout(in []OverviewCard) []OverviewCard {
	seen := map[string]bool{}
	out := make([]OverviewCard, 0, len(OverviewCards))
	add := func(c OverviewCard) {
		def, ok := OverviewCardDefByID(c.ID)
		if !ok || seen[c.ID] {
			return
		}
		seen[c.ID] = true
		var opts map[string]string
		for _, od := range def.Options {
			for _, ch := range od.Choices {
				if c.Options[od.Key] == ch.Value {
					if opts == nil {
						opts = map[string]string{}
					}
					opts[od.Key] = ch.Value
				}
			}
		}
		out = append(out, OverviewCard{ID: c.ID, Enabled: c.Enabled, Options: opts})
	}
	for _, c := range in {
		add(c)
	}
	for _, d := range OverviewCards {
		add(OverviewCard{ID: d.ID, Enabled: true})
	}
	return out
}

// ParseOverviewLayout reads the stored JSON. Empty means the default layout;
// invalid JSON is an error (Validate uses this). The result is normalized.
func ParseOverviewLayout(raw string) ([]OverviewCard, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultOverviewLayout(), nil
	}
	var cards []OverviewCard
	if err := json.Unmarshal([]byte(raw), &cards); err != nil {
		return nil, fmt.Errorf("overview layout: %w", err)
	}
	return NormalizeOverviewLayout(cards), nil
}

// EncodeOverviewLayout is the stored form: normalized JSON.
func EncodeOverviewLayout(cards []OverviewCard) string {
	b, _ := json.Marshal(NormalizeOverviewLayout(cards))
	return string(b)
}

// OverviewCards returns the configured layout. A stored value that does not
// parse falls back to the default rather than hiding the whole page.
func (c Config) OverviewCards() []OverviewCard {
	cards, err := ParseOverviewLayout(c.OverviewLayout)
	if err != nil {
		return DefaultOverviewLayout()
	}
	return cards
}

// How the Overview treats suspected CGM artifacts.
const (
	// ArtifactFlagged marks suspected artifacts but leaves every number alone.
	ArtifactFlagged = "flagged"
	// ArtifactExclude also leaves them out of the statistics.
	ArtifactExclude = "exclude"
)

// ArtifactsMode returns the configured handling of suspected artifacts.
func (c Config) ArtifactsMode() string {
	if c.ArtifactMode == ArtifactExclude {
		return ArtifactExclude
	}
	return ArtifactFlagged
}

func validArtifactMode(m string) bool {
	return m == "" || m == ArtifactFlagged || m == ArtifactExclude
}

// OverviewRanges are the Overview range presets, in display order.
var OverviewRanges = []string{"7d", "14d", "30d", "90d", "all"}

// DefaultOverviewRange is the preset used when nothing is chosen.
const DefaultOverviewRange = "30d"

// OverviewRange returns the configured default preset.
func (c Config) OverviewRange() string {
	if validOverviewRange(c.OverviewDefaultRange) && c.OverviewDefaultRange != "" {
		return c.OverviewDefaultRange
	}
	return DefaultOverviewRange
}

func validOverviewRange(r string) bool {
	if r == "" {
		return true
	}
	for _, v := range OverviewRanges {
		if r == v {
			return true
		}
	}
	return false
}
