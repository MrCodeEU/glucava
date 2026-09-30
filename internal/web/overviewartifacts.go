package web

import (
	"net/url"
	"strconv"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/store"
)

// This file is the Overview's handling of suspected CGM artifacts: badges on
// the episodes they explain, the note that says what the numbers would be
// without them, the list of what was left out, and the buttons that record a
// manual verdict.

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
	tr := d.tr()
	m := d.Model
	if len(m.Artifacts) == 0 {
		return g.Group(nil)
	}
	n := len(m.Artifacts)
	if m.Excluded {
		return P(Class("muted text-sm"), g.Text(tr.Tn("artifact.note.excluded", n, "clean", tr.Num(m.BelowClean, 1), "all", tr.Num(m.BelowAll, 1))),
			A(Href("/settings"), g.Text(tr.T("artifact.note.change"))), g.Text("."))
	}
	return P(Class("muted text-sm"), g.Text(tr.Tn("artifact.note.flagged", n, "clean", tr.Num(m.BelowClean, 1), "all", tr.Num(m.BelowAll, 1))),
		g.Text(tr.T("artifact.note.hint")))
}

// leftOutList lists the suspected artifacts that were excluded from the
// numbers, each with a button to say it was real after all.
func leftOutList(d StatsData) g.Node {
	tr := d.tr()
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
			Td(g.Text(fmtWhenT(tr, a.Start, d.Loc, d.Now))),
			Td(Badge("info", artifactLabelT(tr, a))),
			Td(Class("num"), g.Text(valueT(tr, a.Nadir, d.Unit))),
			Td(Span(Class("muted"), g.Text(artifactReasonT(tr, a, d.Unit)))),
			Td(d.markButton(a.Start, a.End, store.MarkReal, tr.T("artifact.btn.real"))),
		))
	}
	return Div(H3(Class("mt-4"), g.Text(tr.T("artifact.left_out.title"))),
		Div(append(comp("tablewrap"), Table(append(comp("table"),
			THead(Tr(Th(g.Text(tr.T("overview.episodes.when"))), Th(g.Text(tr.T("overview.episodes.kind"))), Th(Class("num"), g.Text(tr.T("artifact.left_out.nadir"))), Th(g.Text(tr.T("artifact.left_out.why"))), Th(g.Text("")))),
			TBody(g.Group(rows)))...))...))
}

// episodeActions are the verdict buttons of a low episode.
func episodeActions(e analytics.Episode, art *analytics.Artifact, d StatsData) g.Node {
	if e.Kind != analytics.KindLow {
		return g.Group(nil)
	}
	tr := d.tr()
	switch {
	case art == nil:
		return d.markButton(e.Start, e.End, store.MarkArtifact, tr.T("artifact.btn.not_real"))
	case art.Marked:
		return d.markButton(e.Start, e.End, store.MarkReal, tr.T("artifact.btn.real"))
	}
	return g.Group([]g.Node{
		d.markButton(e.Start, e.End, store.MarkArtifact, tr.T("artifact.btn.not_real")),
		d.markButton(e.Start, e.End, store.MarkReal, tr.T("artifact.btn.real")),
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
func artifactModeField(tr *i18n.Translator) g.Node {
	return Field("artifactMode", tr.T("artifact.mode.label"), tr.T("artifact.mode.help"),
		g.El("select", ID("artifactMode"), g.Attr("data-bind", "artifactMode"),
			Option(Value(store.ArtifactFlagged), g.Text(tr.T("artifact.mode.flag"))),
			Option(Value(store.ArtifactExclude), g.Text(tr.T("artifact.mode.exclude")))))
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
	tr := d.tr()
	a := d.Artifacts[0]
	at := fmtWhenT(tr, a.Start, d.Loc, d.Now)
	more := ""
	if n := len(d.Artifacts); n > 1 {
		more = " " + tr.T("artifact.notice.more", "n", n-1)
	}
	return Notice("warning", Strong(g.Text(tr.T("artifact.notice.title"))),
		g.Text(tr.T("artifact.notice.body", "label", artifactLabelT(tr, a), "at", at, "more", more, "detail", artifactDetailT(tr, a, render.Unit(d.Cfg.Unit)))),
		A(Href("/stats#ov-episodes"), g.Text(tr.T("artifact.notice.link"))), g.Text("."))
}
