# Vendored font: Inter

The PDF report (`../render.go`) is typeset with an embedded font, so its output
does not depend on what the host has installed.

- **Family:** Inter 4.1, Regular (400) and Bold (700)
- **Source:** https://github.com/rsms/inter/releases/tag/v4.1, `Inter-4.1.zip`
  (sha256 `9883fdd4a49d4fb66bd8177ba6625ef9a64aa45899767dde3d36aa425756b11e`),
  files `extras/ttf/Inter-Regular.ttf` and `extras/ttf/Inter-Bold.ttf`
- **License:** SIL Open Font License 1.1, `OFL.txt` (copied from the release's
  `LICENSE.txt`). Embedding and redistributing the subsets is allowed; the
  files are modified (subset), which the OFL permits, and keep the family name
  "Inter" because they are not sold on their own.

## Subset

Both files are subset to what the report prints: Basic Latin, Latin-1
Supplement, en and em dash, curly quotes, bullet, ellipsis, minus sign
(U+2212), less/greater-or-equal, arrows (U+2191, U+2192, U+2193) and the
approximately sign. Layout features kept: `kern`, `liga`, `tnum`, `case`.
Hinting is dropped. Each file is about 34 KB.

```sh
pip install fonttools
for w in Regular Bold; do
  pyftsubset extras/ttf/Inter-$w.ttf \
    --unicodes="U+0020-007E,U+00A0-00FF,U+2013,U+2014,U+2018,U+2019,U+201C,U+201D,U+2022,U+2026,U+2212,U+2264,U+2265,U+2192,U+2191,U+2193,U+2248" \
    --layout-features='kern,liga,tnum,case' --no-hinting \
    --output-file=Inter-$w.ttf
done
```

| File | sha256 |
| --- | --- |
| `Inter-Regular.ttf` | `389fb869b87cecd5c8f5ef2d99724ce7dda09e443c45be185f4d12264b366006` |
| `Inter-Bold.ttf` | `c638d333151e6b844bec3da74391908e1fe140dc21d4e29d5bbfc25b6888ebb3` |

A character outside the subset renders as a missing glyph in the PDF. Add it to
the `--unicodes` list and regenerate before using a new symbol in the template
data.

## Languages

The subset already covers German: ä ö ü Ä Ö Ü ß and the non-breaking space are
in Latin-1. `TestTemplateTextStaysInsideTheFontSubset` builds the report data
in English and German and fails on any character outside the subset. A locale
that needs more glyphs (the German low quote U+201E, a non-Latin script) must
extend `--unicodes` above and `inSubset` in `../data.go`, then regenerate the
two files.
