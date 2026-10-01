package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

var de = i18n.Default().For("de")

// noKeyLeak fails when a translation key name or an unfilled placeholder
// shows up in a message.
func noKeyLeak(t *testing.T, m notify.Message) {
	t.Helper()
	all := m.Title + "\n" + m.Body + "\n" + m.ChartAlt
	for _, f := range m.Facts {
		all += "\n" + f.Label + ": " + f.Value
	}
	for _, bad := range []string{"email.", "notify.", "{"} {
		if strings.Contains(all, bad) {
			t.Errorf("message leaks %q: %s", bad, all)
		}
	}
}

func TestActivityMessageGerman(t *testing.T) {
	a := act("1", time.Date(2026, 9, 28, 7, 15, 0, 0, vienna), 94, 71.5, 0)
	m, ok := ActivityMessage(de, a, render.MmolL, stats.DefaultRange, nil, vienna)
	if !ok {
		t.Fatal("no message")
	}
	noKeyLeak(t, m)
	if m.Title != "Aktivitätsübersicht: Run 1" || !strings.Contains(m.Body, "wurde auf Strava ergänzt") || !strings.Contains(m.Body, "(Run)") {
		t.Errorf("title/body = %q / %q", m.Title, m.Body)
	}
	if factOf(m, "Zeit im Zielbereich") != "94 %" || factOf(m, "Tiefstwert") != "4,0 mmol/L" {
		t.Errorf("facts = %+v", m.Facts)
	}
	if !strings.Contains(m.Body, "Mo, 28. Sep, 07:15") {
		t.Errorf("date not in German form: %q", m.Body)
	}
}

func TestWeeklyMessageGerman(t *testing.T) {
	cur := []jobs.Activity{act("a", time.Date(2026, 9, 15, 7, 0, 0, 0, vienna), 90, 70, 0), act("b", time.Date(2026, 9, 18, 7, 0, 0, 0, vienna), 70, 60, 5)}
	prev := []jobs.Activity{act("c", time.Date(2026, 9, 9, 7, 0, 0, 0, vienna), 95, 80, 0)}
	from, to := time.Date(2026, 9, 14, 0, 0, 0, 0, vienna), time.Date(2026, 9, 21, 0, 0, 0, 0, vienna)
	m, _ := WeeklyMessage(de, cur, prev, from, to, render.MgDL, vienna)
	noKeyLeak(t, m)
	if m.Title != "Wochenübersicht, 14. Sep – 20. Sep" {
		t.Errorf("title = %q", m.Title)
	}
	if factOf(m, "Aktivitäten mit Unterzuckerung") != "1 von 2" || !strings.HasSuffix(factOf(m, "Änderung zur Vorwoche"), "Prozentpunkte") {
		t.Errorf("facts = %+v", m.Facts)
	}
	if !strings.Contains(factOf(m, "Beste Zeit im Zielbereich"), "Di 15. Sep") {
		t.Errorf("best = %q", factOf(m, "Beste Zeit im Zielbereich"))
	}
	empty, _ := WeeklyMessage(de, nil, nil, from, to, render.MgDL, vienna)
	noKeyLeak(t, empty)
	if !strings.Contains(empty.Body, "keine Aktivität") {
		t.Errorf("empty body = %q", empty.Body)
	}
}

func TestHealthMessageGerman(t *testing.T) {
	in := HealthInput{
		Activities: []jobs.Activity{{StravaID: "1", Start: augFrom.AddDate(0, 0, 2), Status: jobs.StatusFailed}},
		Samples:    hourly(31 * 12), From: augFrom, To: augTo, Build: "v1",
	}
	m := HealthMessage(de, in, vienna)
	noKeyLeak(t, m)
	if m.Title != "Monatlicher Zustandsbericht, August 2026" || m.Severity != "warning" ||
		!strings.Contains(m.Body, "1 Aktivität ist fehlgeschlagen und die Glukosedaten deckten nur 50 % des Monats ab") {
		t.Errorf("title/body/sev = %q / %q / %s", m.Title, m.Body, m.Severity)
	}
	if factOf(m, "Fehlgeschlagene Aktivitäten") != "1" {
		t.Errorf("facts = %+v", m.Facts)
	}
}
