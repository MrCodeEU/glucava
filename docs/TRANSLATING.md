# Translating glucava

glucava's interface language comes from plain JSON files in
`internal/i18n/locales/`. Adding a language needs **no Go code**: the files are
embedded and discovered at startup, and the new language shows up in
Settings > Language with its own name.

English (`en.json`) is the source of truth. Every other file only translates
its values.

## Add a language

1. Scaffold the file from the repository root (copies `en.json` so you can
   translate in place):

   ```sh
   go run ./tools/i18n new fr French Français
   ```

   Or copy `internal/i18n/locales/en.json` to `<tag>.json` yourself. The tag is
   a BCP 47 language tag: `fr`, `pt-BR`, `de-AT`.
2. Fill in `_meta`:

   ```json
   "_meta": {
     "name": "French",
     "native_name": "Français",
     "translators": ["Your Name"],
     "fallback": "en"
   }
   ```

   `native_name` is what users see in the language list. `fallback` is the
   language to use for keys you have not translated (default `en`). A regional
   variant can list only the words that differ and set `fallback` to its parent:
   `de-AT.json` has two keys and `"fallback": "de"`.
3. Translate the **values**. Never change a key.
4. Check it, and look at it running:

   ```sh
   make i18n-check      # keys, placeholders, plurals; shows coverage per language
   make mock            # demo data, login demo@example.test; Settings > Language
   ```

   `make mock` follows your browser's language while Language is "Automatic",
   so you can also just set the browser language.
5. Open a pull request (checklist below).

An untranslated key is not an error: it falls back to the `fallback` language,
then English, so a partial translation is useful and welcome. Keys you leave
out count as missing in `go run ./tools/i18n`. Note that a scaffolded file
counts as complete even while it still contains English text.

## Rules

- **Keys** are lower-case, dot-separated, grouped by where they appear:
  `nav.settings`, `events.col.when`, `login.error.wrong`. Only translate the
  value on the right.
- **Placeholders** look like `{name}` and are filled in by the program. Keep
  them exactly, but you may move them: `"Signed in as {user}"` can become
  `"{user} ist angemeldet"`. Do not add placeholders that English lacks.
- **Plurals** use key suffixes `.zero .one .two .few .many .other`. Provide the
  categories your language needs (English: `one`, `other`; Polish: `one`, `few`,
  `many`, `other`). `.other` is always required. `{n}` is the number.

  ```json
  "items.one": "{n} Eintrag",
  "items.other": "{n} Einträge"
  ```
- **Formats** live under `format.*`: weekday and month names (short and long),
  date and time order, the decimal and thousands separators and how lists are
  joined. Set them to what your country writes: `format.decimal` is `","` in
  German. `format.time` is a Go time layout; `"15:04"` is the 24 hour clock.
  `format.when` decides the order in `Mi, 30. Sep, 21:34`.
- **Tone:** match the existing language files. German uses informal "du".
- **Length:** the phone tab bar is narrow. Keep `nav.*` labels short
  (about 13 characters); longer text is cut off with an ellipsis there.
- Do not translate product names (glucava, Strava, Dexcom, Nightscout), units
  (`mg/dL`) or the Strava description template. The template is the user's own
  text; only the names and descriptions of the presets are translated.
- Do not translate `_meta` keys, only their values.

## Pull request checklist

- [ ] `internal/i18n/locales/<tag>.json` is valid JSON with a filled-in `_meta`
- [ ] `make i18n-check` reports no errors (warnings for missing keys are fine)
- [ ] Every `{placeholder}` is kept; plural groups have `.other`
- [ ] `format.*` values match local conventions
- [ ] You opened the app in your language at desktop and phone width and
      nothing is cut off or wrong
- [ ] You added yourself to `translators` in `_meta`

## For developers

Never add a user-visible string without a key. The rules and helpers:

- In a page: `t(pd, "events.empty")`, `t(pd, "footer.text", "build", pd.Build)`
  (alternating name/value arguments). Plurals: `tn(pd, "items", n)`.
- Outside pages (handlers, SSE patches, notifications): `s.tr(r).T("key")`.
  The request's translator comes from middleware (Settings > Language, else
  the browser's Accept-Language, else English) and is used for live Datastar
  patches too.
- Keys must be string literals so they can be checked. A table of keys uses
  `i18n.Key("a.b")` for each entry; the lookup line then carries
  `// i18n:dynamic`.
- Add the key to `en.json` (and `de.json`) in the same change. `make i18n-check`
  fails on a key missing from `en.json`, on keys no code uses, on extra keys in
  another language, and on placeholder mismatches.
- Dates and numbers: `tr.When`, `tr.Weekday`, `tr.Month`, `tr.Num`, `tr.List`.
  Never `time.Format("Mon Jan")` or `strconv` for text a user reads.
- When you convert a file, add it to `convertedFiles` in
  `internal/web/i18n_lint_test.go`; the test then rejects new hard-coded English
  in it.
- Texts made without a request (alerts, summary emails, the PDF report) have
  no browser to ask. Notifications and emails use the installation language
  (Settings > Language; "auto" means English) through `notify.Translator`; the
  PDF takes `report.Input.T`. `internal/notify/i18n_test.go` rejects a hard-coded
  English `Label`, `Title` or `Body` in `notify`, `digest` and `report`.
- A stored event keeps an English `Message` and also a `MsgKey` with `MsgArgs`
  (`jobs.Event`). Store arguments neutral: minutes in a `*_min` argument and an
  RFC 3339 time in a `*_at` argument; `eventmsg.Render` formats them for the
  reader. Add the key to `internal/eventmsg` so the key check sees it.
