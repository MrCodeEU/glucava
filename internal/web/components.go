// Package web is the browser UI: gomponents views, Datastar for live updates and
// actions, and PocketBase users for login.
package web

import (
	"fmt"
	"time"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

// comp starts an element's attribute list with its data-component marker.
func comp(name string, extra ...g.Node) []g.Node {
	return append([]g.Node{g.Attr("data-component", name)}, extra...)
}

// Card is a bordered surface.
func Card(children ...g.Node) g.Node { return Div(append(comp("card"), children...)...) }

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
