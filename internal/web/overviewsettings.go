package web

import (
	"fmt"
	"strings"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/store"
)

// The Overview layout lives in the settings form as three signals:
// overviewOrder (the card ids in order), overviewOn (id to enabled) and
// overviewOpt ("<id>_<option>" to the chosen value). The settings handler
// turns them back into the stored JSON with settingsSignals.overviewLayout.

// overviewSignals is the initial value of the layout signals for cfg.
func overviewSignals(cfg store.Config) map[string]any {
	cards := cfg.OverviewCards()
	order := make([]string, len(cards))
	on := map[string]bool{}
	opt := map[string]string{}
	for i, c := range cards {
		order[i], on[c.ID] = c.ID, c.Enabled
		if def, ok := store.OverviewCardDefByID(c.ID); ok {
			for _, od := range def.Options {
				v := od.Choices[0].Value
				if c.Options[od.Key] != "" {
					v = c.Options[od.Key]
				}
				opt[c.ID+"_"+od.Key] = v
			}
		}
	}
	return map[string]any{
		"overviewOrder": strings.Join(order, ","), "overviewOn": on, "overviewOpt": opt, "overviewRange": cfg.OverviewRange(), "artifactMode": cfg.ArtifactsMode(),
	}
}

// overviewLayout is the stored JSON for the form's layout signals.
func (v settingsSignals) overviewLayout() string {
	if v.OverviewOrder == "" {
		return ""
	}
	var cards []store.OverviewCard
	for _, id := range strings.Split(v.OverviewOrder, ",") {
		def, ok := store.OverviewCardDefByID(strings.TrimSpace(id))
		if !ok {
			continue
		}
		c := store.OverviewCard{ID: def.ID, Enabled: v.OverviewOn[def.ID]}
		for _, od := range def.Options {
			if val := v.OverviewOpt[def.ID+"_"+od.Key]; val != "" {
				if c.Options == nil {
					c.Options = map[string]string{}
				}
				c.Options[od.Key] = val
			}
		}
		cards = append(cards, c)
	}
	return store.EncodeOverviewLayout(cards)
}

// orderMoveExpr swaps card id with its neighbour in $overviewOrder, one step
// toward the front (dir=-1) or back (dir=+1); see panelMoveExpr, which does
// the same for the chart panels.
func orderMoveExpr(signal, id string, dir int) string {
	return fmt.Sprintf(
		"var a=$%s.split(','); var i=a.indexOf(%q); var j=i+(%d); "+
			"if(i>=0 && j>=0 && j<a.length){var t=a[i]; a[i]=a[j]; a[j]=t; $%s=a.join(',')}",
		signal, id, dir, signal)
}

// overviewCardRow is one line of the Overview layout list: a checkbox, the
// card's name, its options, and up/down buttons. The row's CSS order follows
// its position in $overviewOrder, so the list visibly reorders.
func overviewCardRow(tr *i18n.Translator, def store.OverviewCardDef) g.Node {
	title := cardTitleT(tr, def)
	opts := make([]g.Node, 0, len(def.Options))
	for _, od := range def.Options {
		choices := make([]g.Node, len(od.Choices))
		for i, ch := range od.Choices {
			choices[i] = Option(Value(ch.Value), g.Text(cardText(tr, def.ID+".opt."+od.Key+"."+ch.Value, ch.Label)))
		}
		opts = append(opts, Label(Class("flex items-center gap-1.5 text-sm text-ink-2"), g.Text(cardText(tr, def.ID+".opt."+od.Key, od.Label)),
			g.El("select", append([]g.Node{g.Attr("data-bind", "overviewOpt."+def.ID+"_"+od.Key), ID("overviewOpt_" + def.ID + "_" + od.Key)}, choices...)...)))
	}
	return Div(Class("mb-2 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-line px-3 py-2"),
		g.Attr("data-style:order", fmt.Sprintf("$overviewOrder.split(',').indexOf(%q)", def.ID)),
		Input(Type("checkbox"), ID("overviewOn_"+def.ID), g.Attr("data-bind", "overviewOn."+def.ID), g.Attr("aria-label", tr.T("overview.settings.show", "title", title))),
		Div(Class("min-w-40 flex-1"),
			Div(Class("font-semibold"), g.Text(title)),
			Div(Class("text-xs text-ink-2"), g.Text(cardHelpT(tr, def)))),
		g.Group(opts),
		Div(Class("flex gap-1"),
			BtnSized("", "sm", tr.T("overview.settings.earlier"), ID("overview-move-"+def.ID+"-up"), g.Attr("data-on:click", orderMoveExpr("overviewOrder", def.ID, -1))),
			BtnSized("", "sm", tr.T("overview.settings.later"), ID("overview-move-"+def.ID+"-down"), g.Attr("data-on:click", orderMoveExpr("overviewOrder", def.ID, 1))),
		),
	)
}

// overviewSettingsCard is the "Overview page" card of the settings form.
func overviewSettingsCard(tr *i18n.Translator, cfg store.Config) g.Node {
	rangeOpts := make([]g.Node, 0, len(store.OverviewRanges))
	for _, k := range store.OverviewRanges {
		rangeOpts = append(rangeOpts, Option(Value(k), g.Text(rangeLabelT(tr, k))))
	}
	rows := make([]g.Node, 0, len(store.OverviewCards))
	for _, c := range cfg.OverviewCards() { // saved order; the row's CSS order takes over once edited
		if def, ok := store.OverviewCardDefByID(c.ID); ok {
			rows = append(rows, overviewCardRow(tr, def))
		}
	}
	return Card(H2(g.Text(tr.T("overview.settings.title"))),
		P(Class("muted"), g.Text(tr.T("overview.settings.lead"))),
		Field("overviewRange", tr.T("overview.settings.opens_with"), tr.T("overview.settings.opens_with_help"),
			g.El("select", append([]g.Node{ID("overviewRange"), g.Attr("data-bind", "overviewRange")}, rangeOpts...)...)),
		artifactModeField(tr),
		Label(g.Text(tr.T("overview.settings.cards"))),
		Div(Class("flex flex-col"), g.Group(rows)),
	)
}
