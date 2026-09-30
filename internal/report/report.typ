// glucava glucose report. Compiled by `typst compile` with data.json and the
// chart SVGs written next to this file; see render.go. No packages, no
// network: everything comes from this directory.
#let d = json("data.json")

#let ink = rgb("#1f2933")
#let muted = rgb("#52606d")
#let line-c = rgb("#d9dee3")
#let tile-bg = rgb("#f6f8fa")
#let good = rgb("#1b7f57")
#let bad = rgb("#b54708")

#set document(title: d.title + " " + d.range, author: "glucava")
#set page(
  paper: "a4",
  margin: (x: 16mm, top: 14mm, bottom: 18mm),
  footer: [
    #set text(size: 7.5pt, fill: muted)
    #line(length: 100%, stroke: 0.5pt + line-c)
    #v(-2pt)
    #grid(
      columns: (1fr, auto),
      [#d.disclaimer],
      align(right)[#d.version · #context counter(page).display("1 / 1", both: true)],
    )
  ],
)
#set text(font: "Inter", size: 9pt, fill: ink, lang: "en")
#set par(leading: 0.55em)

#let section(title, sub: none) = {
  v(9pt)
  block(sticky: true)[
    #text(size: 11.5pt, weight: "bold")[#title]
    #if sub != none [#h(6pt) #text(size: 8pt, fill: muted)[#sub]]
    #v(-3pt)
    #line(length: 100%, stroke: 0.5pt + line-c)
  ]
  v(-2pt)
}

// A table of strings: the first column left aligned, the rest right aligned.
#let dtable(head, rows, widths: none, lefts: (0,)) = {
  let n = head.len()
  table(
    columns: if widths == none { (1fr,) * n } else { widths },
    stroke: (x, y) => if y > 0 { (bottom: 0.4pt + line-c) },
    inset: (x: 4pt, y: 3.2pt),
    fill: (x, y) => if y == 0 { rgb("#eef1f4") },
    align: (x, y) => if x in lefts { left } else { right },
    ..head.map(h => text(size: 7.5pt, weight: "bold", fill: muted)[#upper(h)]),
    ..rows.flatten().map(c => text(size: 8.5pt)[#c]),
  )
}

#let tile(k) = block(
  width: 100%,
  fill: tile-bg,
  stroke: 0.5pt + line-c,
  radius: 4pt,
  inset: (x: 9pt, y: 6.5pt),
)[
  #text(size: 7.5pt, weight: "bold", fill: muted)[#upper(k.label)] \
  #v(1pt)
  #text(size: 18pt, weight: "bold")[#k.value] \
  #text(size: 7.5pt, fill: muted)[#k.sub]
  #if k.delta != "" [
    \ #text(size: 7.5pt, weight: "bold", fill: if k.tone == "good" { good } else if k.tone == "bad" { bad } else { muted })[#k.delta]
  ]
]

// ---- header
#grid(
  columns: (1fr, auto),
  align: (left + bottom, right + bottom),
  [
    #text(size: 22pt, weight: "bold")[#d.title] \
    #v(2pt)
    #text(size: 11pt, fill: muted)[#d.range · #d.days]
  ],
  text(size: 8pt, fill: muted)[
    #d.generated \
    All glucose values in #d.unit
    #if d.compare != "" [ \ #d.compare]
  ],
)
#v(4pt)
#line(length: 100%, stroke: 1.2pt + ink)

#if not d.hasData [
  #v(20pt)
  #for n in d.notes [#n]
] else [

#v(8pt)
#grid(columns: (1fr, 1fr, 1fr), gutter: 7pt, ..d.kpis.map(tile))

#section("Time in range", sub: "Share of all readings in each band")
#image("tir.svg", width: 100%)
#v(2pt)
#table(
  columns: (auto, 1.4fr, auto, 2fr),
  stroke: (x, y) => (bottom: 0.4pt + line-c),
  inset: (x: 4pt, y: 3pt),
  align: (x, y) => if x == 2 { right } else { left },
  ..d.bands.map(b => (
    [#box(width: 7pt, height: 7pt, radius: 1.5pt, fill: rgb(b.color)) #h(3pt) #b.label],
    text(fill: muted)[#b.range],
    text(weight: "bold")[#b.pct],
    text(fill: if b.met { good } else { bad }, size: 8pt)[#b.target #if b.met [(met)] else [(not met)]],
  )).flatten(),
)

#section("Daily profile (AGP)", sub: d.agpNote)
#image("agp.svg", width: 100%)

#section("Day by day", sub: "Average glucose with the daily lowest and highest, and time in range per day")
#image("trend.svg", width: 100%)

#section("Time of day", sub: "Share of readings in each band by local time")
#image("parts.svg", width: 100%)
#v(2pt)
#dtable(d.partsHead, d.parts, widths: (2fr, 1fr, 1fr, 1fr, 1fr))

#section("Lows and highs", sub: "Runs of at least 15 minutes beyond a threshold")
#dtable(d.episodesHead, d.episodes, widths: (2.4fr, 1fr, 1fr, 1fr, 1.2fr, 1.4fr))
#if d.recent.len() > 0 [
  #v(6pt)
  #text(size: 8.5pt, weight: "bold")[Most recent]
  #v(2pt)
  #dtable(d.recentHead, d.recent, widths: (2.2fr, 1fr, 1fr, 1.4fr, 2.2fr), lefts: (0, 4))
]

#if d.sports.len() > 0 [
  #section("By activity type", sub: "Per-activity averages. Change is start to end; Drop is per 10 min; Lows is the share followed by a low within 3 h")
  #dtable(d.sportsHead, d.sports, widths: (1.4fr, 0.8fr, 0.9fr, 0.8fr, 0.9fr, 0.9fr, 0.9fr, 1fr, 1.2fr, 1fr, 1.2fr))
]

#if d.acts.len() > 0 [
  #section("Activities", sub: d.actsNote)
  #dtable(d.actsHead, d.acts, widths: (1.7fr, 3fr, 1fr, 1fr, 1.4fr, 0.8fr), lefts: (0, 1))
]

#if d.sources.len() > 0 [
  #section("Glucose sources")
  #dtable(d.sourcesHead, d.sources, widths: (2fr, 1.5fr, 2fr))
]

#v(8pt)
#set text(size: 7.5pt, fill: muted)
#for n in d.notes [#n \ ]

]
