// Package web is the browser UI: gomponents views, Datastar for live updates and
// actions, and PocketBase users for login.
package web

import (
	"fmt"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

// Styling model. The stylesheet is Tailwind (v4) compiled into
// static/app.css by `make css`; the compiled file is committed, so building or
// running glucava never needs Tailwind.
//
// A component is an element with a data-component attribute (comp) plus the
// utility classes listed for it in compClasses. Variants stay data attributes
// (data-variant, data-size, data-cols, ...) and are styled with Tailwind's
// data-[variant=x]: modifiers, so tests and Datastar patches can still read
// them. What utilities cannot express (descendants without a class of their
// own, SVG internals, the theme variables) lives in static/input.css.
//
// IMPORTANT: every class string in Go must be a whole literal such as
// "px-3 py-2". The Tailwind scanner reads source text, so a class assembled
// at run time ("px-"+n, fmt.Sprintf("text-%s", tone)) is never generated and
// silently does nothing. To choose between styles, pick between complete
// literals (a map or a switch), then run `make css` and commit app.css;
// `make css-check` (CI) fails when it is stale.
//
// Never give one element both comp() and Class(...): gomponents would emit two
// class attributes and the browser keeps only the first (TestNoDuplicateClass).

// compClasses maps a component (or "component:part") to its utility classes.
var compClasses = map[string]string{
	"shell":  "flex min-h-screen flex-col",
	"banner": "bg-warn px-3 py-1.5 text-center text-xs font-semibold text-bg",
	"footer": "px-4 pb-24 pt-6 text-center text-xs text-ink-2 md:pb-6",
	"card":   "mb-4 rounded-card border border-line bg-surface p-4 shadow-card last:mb-0 sm:p-5",
	"grid": "mb-4 grid grid-cols-[repeat(auto-fit,minmax(min(9.5rem,100%),1fr))] gap-4 [&>*]:mb-0 " +
		"data-[cols=2]:grid-cols-[repeat(auto-fit,minmax(min(20rem,100%),1fr))] " +
		"data-[cols=photo]:grid-cols-1 data-[cols=photo]:items-start md:data-[cols=photo]:grid-cols-[minmax(0,28.75rem)_minmax(0,1fr)]",
	"pagehead":   "mb-5 flex flex-wrap items-start justify-between gap-3 [&_p]:mb-0",
	"actions":    "flex flex-wrap items-center gap-2",
	"tablewrap":  "max-h-[75vh] overflow-auto",
	"table":      "w-full border-collapse text-sm",
	"empty":      "rounded-lg border border-dashed border-line px-4 py-10 text-center text-ink-2",
	"field":      "mb-3.5",
	"fieldrow":   "grid grid-cols-[repeat(auto-fit,minmax(9rem,1fr))] gap-x-3",
	"check":      "mb-3.5 flex items-center gap-2 font-semibold",
	"secretbox":  "my-3 rounded-lg border border-dashed border-accent bg-surface-2 p-3",
	"loginwrap":  "grid min-h-screen place-items-center p-4 [&_[data-component=card]]:w-full [&_[data-component=card]]:max-w-sm",
	"dl":         "mb-3 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5",
	"tirbar":     "flex h-2 w-28 overflow-hidden rounded-full bg-surface-2",
	"tircell":    "flex items-center gap-2.5",
	"sportbar":   "flex items-center gap-2.5 py-1.5",
	"chart":      "block h-auto w-full",
	"trendchart": "block h-36 w-full",
	"legend":     "mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-ink-2",

	"button": "inline-flex cursor-pointer items-center justify-center gap-1.5 rounded-lg border border-line bg-surface px-3.5 py-2 " +
		"text-sm font-semibold leading-tight text-ink no-underline shadow-xs transition hover:no-underline " +
		"not-data-[variant]:hover:bg-surface-2 disabled:cursor-progress disabled:opacity-55 aria-busy:cursor-progress aria-busy:opacity-55 " +
		"data-[variant=primary]:border-accent data-[variant=primary]:bg-accent data-[variant=primary]:text-accent-ink data-[variant=primary]:hover:brightness-110 " +
		"data-[variant=danger]:text-bad data-[variant=danger]:hover:bg-bad/10 " +
		"data-[variant=ghost]:border-transparent data-[variant=ghost]:bg-transparent data-[variant=ghost]:shadow-none data-[variant=ghost]:hover:bg-surface-2 " +
		"data-[size=sm]:px-2.5 data-[size=sm]:py-1 data-[size=sm]:text-xs data-[size=lg]:px-5 data-[size=lg]:py-2.5 data-[size=lg]:text-base",

	"badge": "inline-flex items-center gap-1 whitespace-nowrap rounded-full border border-line bg-surface-2 px-2 py-0.5 text-xs font-semibold text-ink-2 " +
		"data-[variant=done]:border-ok/35 data-[variant=done]:bg-ok/10 data-[variant=done]:text-ok " +
		"data-[variant=ok]:border-ok/35 data-[variant=ok]:bg-ok/10 data-[variant=ok]:text-ok " +
		"data-[variant=failed]:border-bad/35 data-[variant=failed]:bg-bad/10 data-[variant=failed]:text-bad " +
		"data-[variant=error]:border-bad/35 data-[variant=error]:bg-bad/10 data-[variant=error]:text-bad " +
		"data-[variant=pending]:border-info/35 data-[variant=pending]:bg-info/10 data-[variant=pending]:text-info " +
		"data-[variant=processing]:border-info/35 data-[variant=processing]:bg-info/10 data-[variant=processing]:text-info " +
		"data-[variant=info]:border-info/35 data-[variant=info]:bg-info/10 data-[variant=info]:text-info " +
		"data-[variant=warning]:border-warn/35 data-[variant=warning]:bg-warn/10 data-[variant=warning]:text-warn",

	"notice": "mb-3 rounded-lg border border-l-4 border-line border-l-info bg-surface-2 px-3.5 py-2.5 text-sm " +
		"data-[variant=error]:border-l-bad data-[variant=warning]:border-l-warn data-[variant=ok]:border-l-ok",

	"toast": "pointer-events-auto animate-[gv-toast-in_0.18s_ease-out] rounded-lg bg-ink px-3.5 py-2.5 text-sm font-medium text-bg shadow-lg " +
		"data-[variant=error]:bg-bad data-[variant=ok]:bg-ok",

	"confirmdialog":         "w-[min(26rem,calc(100vw-2rem))] rounded-xl border border-line bg-surface p-0 text-ink shadow-2xl",
	"confirmdialog:body":    "p-5",
	"confirmdialog:actions": "mt-4 flex justify-end gap-2",

	"segmented": "inline-flex max-w-full gap-0.5 overflow-x-auto rounded-lg border border-line bg-surface-2 p-0.5",
	"tabs":      "mb-4 flex gap-1 overflow-x-auto border-b border-line",
	"tip":       "relative ml-1 inline-flex cursor-help align-middle text-ink-2 hover:text-ink",
}

// Whole-literal classes for parts that are not comp() elements.
const (
	segmentClass = "whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium text-ink-2 no-underline hover:text-ink hover:no-underline " +
		"aria-[current=page]:bg-surface aria-[current=page]:text-ink aria-[current=page]:shadow-card"
	tabClass = "-mb-px whitespace-nowrap border-b-2 border-transparent px-3 py-2 text-sm font-medium text-ink-2 no-underline hover:text-ink hover:no-underline " +
		"aria-[current=page]:border-accent aria-[current=page]:text-ink"
)

// comp starts an element's attribute list with its data-component marker and
// the component's utility classes.
func comp(name string, extra ...g.Node) []g.Node {
	n := []g.Node{g.Attr("data-component", name)}
	if c := compClasses[name]; c != "" {
		n = append(n, Class(c))
	}
	return append(n, extra...)
}

// Card is a bordered surface.
func Card(children ...g.Node) g.Node { return Div(append(comp("card"), children...)...) }

// ConfirmDialog renders a button that opens a styled native <dialog> asking
// body before doing anything, plus the dialog itself. confirmAttrs go on the
// dialog's own confirm button (e.g. postThenGo), so the action only runs
// once the person actually confirms; Cancel just closes the dialog. id must
// be unique on the page.
//
// A native <dialog> is used, rather than window.confirm(), so the prompt
// matches the rest of the UI; it is opened and closed by calling
// showModal()/close() from an already-permitted data-on:click expression
// (see postConfirmThenGo's comment on why an injected <script> tag cannot be
// used here instead).
func ConfirmDialog(id, variant, label, title, body string, confirmAttrs ...g.Node) g.Node {
	openIt := fmt.Sprintf("document.getElementById(%q).showModal()", id)
	closeIt := fmt.Sprintf("document.getElementById(%q).close()", id)
	return g.Group([]g.Node{
		Btn(variant, label, g.Attr("data-on:click", openIt)),
		g.El("dialog", append(comp("confirmdialog"), ID(id),
			Div(append(comp2("confirmdialog", "body"),
				H3(g.Text(title)),
				P(g.Text(body)),
				Div(append(comp2("confirmdialog", "actions"),
					Btn("", "Cancel", g.Attr("data-on:click", closeIt)),
					Btn(variant, label, confirmAttrs...),
				)...),
			)...),
		)...),
	})
}

// comp2 marks an element as one part of a multi-piece component, e.g. a
// dialog's body or its action row, with data-component, data-part and the
// part's classes ("component:part" in compClasses).
func comp2(name, part string) []g.Node {
	n := []g.Node{g.Attr("data-component", name), g.Attr("data-part", part)}
	if c := compClasses[name+":"+part]; c != "" {
		n = append(n, Class(c))
	}
	return n
}

// Grid lays out cards in responsive columns. cols "2" gives wider columns.
func Grid(cols string, children ...g.Node) g.Node {
	return Div(append(comp("grid", g.If(cols != "", g.Attr("data-cols", cols))), children...)...)
}

// Tile is a headline number.
func Tile(label, value, sub string) g.Node {
	return Div(append(comp("card", g.Attr("data-tile", "")),
		Div(Class("label"), g.Text(label)),
		Div(Class("value"), g.Text(value)),
		g.If(sub != "", Div(Class("sub"), g.Text(sub))),
	)...)
}

// Btn renders a <button>. variant is "", "primary" or "danger".
func Btn(variant, label string, attrs ...g.Node) g.Node {
	return BtnSized(variant, "", label, attrs...)
}

// BtnSized is Button with a size ("sm" or "").
func BtnSized(variant, size, label string, attrs ...g.Node) g.Node {
	n := comp("button",
		g.If(variant != "", g.Attr("data-variant", variant)),
		g.If(size != "", g.Attr("data-size", size)),
		Type("button"),
	)
	n = append(n, attrs...)
	n = append(n, g.Text(label))
	return g.El("button", n...)
}

// IndicatorBtn is Btn for an action that drives a headless browser (Strava
// login, session test, Dexcom test, a poll), which can take several seconds
// with no other feedback. It disables itself and shows a spinner while the
// request is in flight. key is the Datastar indicator signal name and must
// be unique on the page.
func IndicatorBtn(variant, label, action, key string) g.Node {
	n := comp("button",
		g.If(variant != "", g.Attr("data-variant", variant)),
		Type("button"),
		post(action),
		g.Attr("data-indicator:"+key, ""),
		g.Attr("data-attr:disabled", "$"+key),
		g.Attr("data-attr:aria-busy", "$"+key),
	)
	n = append(n, Span(Class("spinner"), g.Attr("data-show", "$"+key)), g.Text(label))
	return g.El("button", n...)
}

// SubmitBtn is a submit button for plain forms.
func SubmitBtn(variant, size, label string) g.Node {
	n := comp("button",
		g.If(variant != "", g.Attr("data-variant", variant)),
		g.If(size != "", g.Attr("data-size", size)),
		Type("submit"), g.Text(label),
	)
	return g.El("button", n...)
}

// Badge is a small status pill.
func Badge(variant, text string) g.Node {
	return Span(append(comp("badge", g.Attr("data-variant", variant)), g.Text(text))...)
}

// Field is a labelled form control.
func Field(id, label, help string, control g.Node) g.Node {
	return Div(append(comp("field"),
		Label(For(id), g.Text(label)),
		control,
		g.If(help != "", Div(Class("help"), g.Text(help))),
	)...)
}

// Notice is an inline callout. variant: "", "warning" or "error".
func Notice(variant string, children ...g.Node) g.Node {
	return Div(append(comp("notice", g.If(variant != "", g.Attr("data-variant", variant))), children...)...)
}

// Toast is a short message that removes itself.
func Toast(variant, msg string) g.Node {
	return Div(append(comp("toast",
		g.If(variant != "", g.Attr("data-variant", variant)),
		g.Attr("role", "status"),
		g.Attr("data-init", "setTimeout(() => el.remove(), 4500)"),
	), g.Text(msg))...)
}

// PageHead is the title row of a page, with optional actions on the right.
func PageHead(title, sub string, actions ...g.Node) g.Node {
	return Div(append(comp("pagehead"),
		Div(H1(g.Text(title)), g.If(sub != "", P(Class("muted"), g.Text(sub)))),
		g.If(len(actions) > 0, Div(append(comp("actions"), actions...)...)),
	)...)
}

// StatusBadge maps an activity status to a badge.
func StatusBadge(status string) g.Node {
	label := map[string]string{"done": "Done", "failed": "Failed", "pending": "Queued", "processing": "Working"}[status]
	if label == "" {
		label = status
	}
	return Badge(status, label)
}

// SeverityBadge maps an event severity to a badge.
func SeverityBadge(sev string) g.Node {
	switch sev {
	case "error":
		return Badge("error", "Error")
	case "warning":
		return Badge("warning", "Warning")
	}
	return Badge("info", "Info")
}

// fmtDuration renders 3000s as "50 min" or 1h 12m.
func fmtDuration(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

// fmtWhen renders a time in loc, dropping the year for the current one.
func fmtWhen(t time.Time, loc *time.Location, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	t = t.In(loc)
	if t.Year() == now.In(loc).Year() {
		return t.Format("Mon 2 Jan, 15:04")
	}
	return t.Format("2 Jan 2006, 15:04")
}

// Delta is the change-versus-before line under a stat tile. Dir is "up",
// "down" or "flat" (the arrow); Tone is "good", "bad" or "" (neutral colour),
// because a rising number is not always good news.
type Delta struct{ Text, Dir, Tone string }

// StatTile is Tile with an optional trend line.
func StatTile(label, value, sub string, d *Delta) g.Node {
	var delta g.Node
	if d != nil {
		ic := "minus"
		switch d.Dir {
		case "up":
			ic = "arrow-up"
		case "down":
			ic = "arrow-down"
		}
		delta = Div(Class("delta"), g.If(d.Tone != "", g.Attr("data-tone", d.Tone)), icon(ic, "size-3.5"), g.Text(d.Text))
	}
	return Div(append(comp("card", g.Attr("data-tile", "")),
		Div(Class("label"), g.Text(label)),
		Div(Class("value"), g.Text(value)),
		g.If(sub != "", Div(Class("sub"), g.Text(sub))),
		delta,
	)...)
}

// NavItem is one entry of a Segmented control or Tabs bar.
type NavItem struct{ Key, Label, Href string }

// Segmented is a compact pill switch of links (a range picker, a view
// switch). Each option is a normal link, so it works without JavaScript and
// the address bar keeps the choice.
func Segmented(label string, items []NavItem, active string) g.Node {
	links := make([]g.Node, 0, len(items))
	for _, it := range items {
		links = append(links, A(Href(it.Href), Class(segmentClass), g.Text(it.Label),
			g.If(it.Key == active, g.Attr("aria-current", "page"))))
	}
	return Div(append(comp("segmented"), g.Attr("role", "group"), g.Attr("aria-label", label), g.Group(links))...)
}

// Tabs is an underlined tab bar of links, for switching between views of one page.
func Tabs(label string, items []NavItem, active string) g.Node {
	links := make([]g.Node, 0, len(items))
	for _, it := range items {
		links = append(links, A(Href(it.Href), Class(tabClass), g.Text(it.Label),
			g.If(it.Key == active, g.Attr("aria-current", "page"))))
	}
	return Nav(append(comp("tabs"), g.Attr("aria-label", label), g.Group(links))...)
}

// EmptyState is the "nothing here yet" panel: an icon, a title and a hint on
// what to do about it, plus optional buttons.
func EmptyState(ic, title, hint string, actions ...g.Node) g.Node {
	return Div(append(comp("empty"),
		Div(Class("mx-auto mb-2 grid size-10 place-items-center rounded-full bg-surface-2 text-ink-2"), icon(ic, "size-5")),
		Div(Class("font-semibold text-ink"), g.Text(title)),
		g.If(hint != "", P(Class("mx-auto mt-1 mb-0 max-w-md text-sm"), g.Text(hint))),
		g.If(len(actions) > 0, Div(Class("mt-4 flex justify-center gap-2"), g.Group(actions))),
	)...)
}

// ErrorState is EmptyState for something that failed to load.
func ErrorState(title, detail string) g.Node {
	return Div(append(comp("notice", g.Attr("data-variant", "error"), g.Attr("role", "alert")),
		Div(Class("font-semibold"), g.Text(title)),
		g.If(detail != "", Div(Class("mt-0.5 text-ink-2"), g.Text(detail))),
	)...)
}

// Skeleton is a shimmering placeholder block; size it with the classes given
// (whole literals, e.g. "h-24 w-full").
func Skeleton(sizeClass string) g.Node {
	return Div(g.Attr("data-component", "skeleton"), Class("animate-pulse rounded-md bg-surface-2 "+sizeClass), g.Attr("aria-hidden", "true"))
}

// HelpTip is a "?" that shows text on hover or keyboard focus.
func HelpTip(text string) g.Node {
	return Span(append(comp("tip"), g.Attr("tabindex", "0"), g.Attr("aria-label", text),
		icon("help", "size-4"),
		Span(g.Attr("role", "tooltip"), Class("absolute bottom-full left-1/2 z-20 mb-2 w-56 -translate-x-1/2 rounded-lg bg-ink px-3 py-2 text-xs font-normal leading-snug text-bg shadow-lg"), g.Text(text)),
	)...)
}

// iconPaths are Lucide-style 24x24 stroke icons, inlined so there is no icon
// font or extra request.
var iconPaths = map[string]string{
	"activity":   `<path d="M22 12h-4l-3 9L9 3l-3 9H2"/>`,
	"chart":      `<path d="M3 3v18h18"/><path d="M18 17V9"/><path d="M13 17V5"/><path d="M8 17v-3"/>`,
	"bell":       `<path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/>`,
	"sliders":    `<path d="M21 4h-7M10 4H3M21 12h-9M8 12H3M21 20h-5M12 20H3M14 2v4M8 10v4M16 18v4"/>`,
	"more":       `<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>`,
	"sun":        `<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/>`,
	"moon":       `<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>`,
	"logout":     `<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>`,
	"chevron":    `<path d="m6 9 6 6 6-6"/>`,
	"link":       `<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>`,
	"zap":        `<path d="M13 2 3 14h9l-1 8 10-12h-9l1-8z"/>`,
	"scroll":     `<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7z"/><path d="M14 2v5h6"/><path d="M8 13h8M8 17h5"/>`,
	"user":       `<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>`,
	"help":       `<circle cx="12" cy="12" r="10"/><path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3"/><path d="M12 17h.01"/>`,
	"arrow-up":   `<path d="M12 19V5M5 12l7-7 7 7"/>`,
	"arrow-down": `<path d="M12 5v14M19 12l-7 7-7-7"/>`,
	"minus":      `<path d="M5 12h14"/>`,
	"inbox":      `<path d="M22 12h-6l-2 3h-4l-2-3H2"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>`,
	"key":        `<circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6M15.5 7.5l3 3"/>`,
	"alert":      `<path d="m21.73 18-8-14a2 2 0 0 0-3.46 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3z"/><path d="M12 9v4M12 17h.01"/>`,
}

// icon draws a named icon. sizeClass is a whole-literal size class such as
// "size-4"; the icon takes the current text colour.
func icon(name, sizeClass string) g.Node {
	return g.Raw(`<svg class="` + sizeClass + ` inline-block shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + iconPaths[name] + `</svg>`)
}
