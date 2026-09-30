package web

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/store"
)

// This file is the Overview's handling of suspected CGM artifacts: badges on
// the episodes they explain, the note that says what the numbers would be
// without them, the list of what was left out, and the buttons that record a
// manual verdict.

// artifactLabel is the short name of a suspected artifact.
func artifactLabel(a analytics.Artifact) string {
	switch {
	case a.Marked:
		return "marked not real"
	case a.Kind == analytics.ArtifactCompression:
		return "possible compression low"
	case a.Kind == analytics.ArtifactDip:
		return "possible sensor dip"
	}
	return "suspected artifact"
}

// markHref is the action URL that records a verdict on [start, end].
func markHref(start, end time.Time, kind string) string {
	q := url.Values{
		"start": {strconv.FormatInt(start.Unix(), 10)},
		"end":   {strconv.FormatInt(end.Unix(), 10)},
		"kind":  {kind},
	}
	return "/actions/artifact-mark?" + q.Encode()
}

// markDest is where the browser goes after a verdict is saved: the same
// Overview, reloaded (a changing marked= makes the address differ, so the
// browser reloads instead of only jumping to the fragment) and scrolled back
// to the Lows and highs card.
func (d StatsData) markDest() string {
	return d.Range.href(d.Range.Compare) + "&marked=" + strconv.FormatInt(d.Now.UnixMilli(), 10) + "#ov-episodes"
}

func (d StatsData) markButton(start, end time.Time, kind, label string) g.Node {
	return BtnSized("ghost", "sm", label, postThenGo(markHref(start, end, kind), d.markDest()))
}

// artifactNote says what time below range is with and without the suspected
// artifacts; empty when none were found.
func artifactNote(d StatsData) g.Node {
	m := d.Model
	if len(m.Artifacts) == 0 {
		return g.Group(nil)
	}
	n := len(m.Artifacts)
	noun := "suspected sensor artifacts"
	if n == 1 {
		noun = "suspected sensor artifact"
	}
	if m.Excluded {
		return P(Class("muted text-sm"), g.Textf("Time below range is %.1f%% with %d %s left out (%.1f%% as recorded). ",
			m.BelowClean, n, noun, m.BelowAll), A(Href("/settings"), g.Text("Change this in Settings")), g.Text("."))
	}
	return P(Class("muted text-sm"), g.Textf("Time below range is %.1f%% as recorded and %.1f%% without the %d %s. ",
		m.BelowAll, m.BelowClean, n, noun),
		g.Text("Nothing is left out; choose “exclude” under Settings → Overview page to leave them out."))
}

// leftOutList lists the suspected artifacts that were excluded from the
// numbers, each with a button to say it was real after all.
func leftOutList(d StatsData) g.Node {
	m := d.Model
	if !m.Excluded {
		return g.Group(nil)
	}
	n := len(m.Artifacts)
	if n > 8 {
		n = 8
	}
	rows := make([]g.Node, 0, n)
	for i := len(m.Artifacts) - 1; i >= 0 && len(rows) < n; i-- { // newest first
		a := m.Artifacts[i]
		rows = append(rows, Tr(
			Td(g.Text(fmtWhen(a.Start, d.Loc, d.Now))),
			Td(Badge("info", artifactLabel(a))),
			Td(Class("num"), g.Text(render.Value(a.Nadir, d.Unit))),
			Td(Span(Class("muted"), g.Text(a.Reason))),
			Td(d.markButton(a.Start, a.End, store.MarkReal, "It was real")),
		))
	}
	return Div(H3(Class("mt-4"), g.Text("Left out as suspected sensor artifacts")),
		Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text("When")), Th(g.Text("Kind")), Th(Class("num"), g.Text("Nadir")), Th(g.Text("Why")), Th(g.Text("")))),
			TBody(g.Group(rows)))...))...))
}

// episodeActions are the verdict buttons of a low episode.
func episodeActions(e analytics.Episode, art *analytics.Artifact, d StatsData) g.Node {
	if e.Kind != analytics.KindLow {
		return g.Group(nil)
	}
	switch {
	case art == nil:
		return d.markButton(e.Start, e.End, store.MarkArtifact, "Not real")
	case art.Marked:
		return d.markButton(e.Start, e.End, store.MarkReal, "It was real")
	}
	return g.Group([]g.Node{
		d.markButton(e.Start, e.End, store.MarkArtifact, "Not real"),
		d.markButton(e.Start, e.End, store.MarkReal, "It was real"),
	})
}

// artifactFor returns the suspected artifact an episode overlaps, if any.
func artifactFor(m *overviewModel, e analytics.Episode) *analytics.Artifact {
	if e.Kind != analytics.KindLow {
		return nil
	}
	if a, ok := analytics.OverlappingArtifact(m.Artifacts, e.Start, e.End); ok {
		return &a
	}
	return nil
}

// artifactModeField is the settings control that chooses between flagging
// and excluding suspected artifacts.
func artifactModeField() g.Node {
	return Field("artifactMode", "Suspected sensor artifacts",
		"Compression lows and sudden sensor dips are flagged on the Lows and highs card. “Flag only” keeps every number as recorded; “Exclude” also leaves them out of the statistics. You can always mark a low as real or not real by hand.",
		g.El("select", ID("artifactMode"), g.Attr("data-bind", "artifactMode"),
			Option(Value(store.ArtifactFlagged), g.Text("Flag only")),
			Option(Value(store.ArtifactExclude), g.Text("Exclude from statistics"))))
}

// toAnalyticsMarks converts stored verdicts to the analytics package's type.
func toAnalyticsMarks(stored []store.ArtifactMark) []analytics.Mark {
	out := make([]analytics.Mark, 0, len(stored))
	for _, mk := range stored {
		kind := analytics.MarkArtifact
		if mk.Kind == store.MarkReal {
			kind = analytics.MarkReal
		}
		out = append(out, analytics.Mark{Start: mk.Start, End: mk.End, Kind: kind})
	}
	return out
}

// artifactNotice tells the activity page's reader that part of the shown
// glucose window looks like a sensor artifact. The readings are unchanged.
func artifactNotice(d ActivityData) g.Node {
	if len(d.Artifacts) == 0 {
		return g.Group(nil)
	}
	a := d.Artifacts[0]
	at := fmtWhen(a.Start, d.Loc, d.Now)
	more := ""
	if n := len(d.Artifacts); n > 1 {
		more = fmt.Sprintf(" (and %d more)", n-1)
	}
	return Notice("warning", Strong(g.Text("Possible sensor artifact. ")),
		g.Textf("%s at %s%s: %s. The readings are shown as recorded. ", artifactLabel(a), at, more, a.Detail()),
		A(Href("/stats#ov-episodes"), g.Text("Mark it real or not real on the Overview")), g.Text("."))
}
