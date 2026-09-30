// Command i18n helps translators and maintainers.
//
//	go run ./tools/i18n              translation coverage per locale
//	go run ./tools/i18n new fr       scaffold internal/i18n/locales/fr.json from en.json
//	go run ./tools/i18n check        the checks `make i18n-check` runs
//
// Run it from the repository root. See docs/TRANSLATING.md.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/i18n/check"
)

const localeDir = "internal/i18n/locales"

func main() {
	cmd := "coverage"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "coverage":
		coverage(os.Stdout)
	case "check":
		err = runCheck(os.Stdout)
	case "new":
		if len(os.Args) < 3 {
			err = fmt.Errorf("usage: go run ./tools/i18n new <tag> [English name] [native name]")
			break
		}
		err = scaffold(os.Args[2], arg(3), arg(4))
	default:
		err = fmt.Errorf("unknown command %q (coverage, check, new)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "i18n:", err)
		os.Exit(1)
	}
}

func arg(i int) string {
	if i < len(os.Args) {
		return os.Args[i]
	}
	return ""
}

var pluralRe = regexp.MustCompile(`\.(zero|one|two|few|many|other)$`)

// units counts translatable messages: a plural group is one unit, whatever the
// number of variants a language needs.
func units(keys []string) map[string]bool {
	u := map[string]bool{}
	for _, k := range keys {
		u[pluralRe.ReplaceAllString(k, "")] = true
	}
	return u
}

func coverage(w io.Writer) {
	b := i18n.Default()
	total := units(b.Locale(i18n.Base).Keys())
	fmt.Fprintf(w, "%-8s %-28s %s\n", "locale", "name", "translated")
	for _, info := range b.Locales() {
		own := units(b.Locale(info.Tag).Keys())
		n := 0
		for k := range total {
			if own[k] {
				n++
			}
		}
		note := ""
		if fb := b.Locale(info.Tag).Meta.Fallback; fb != "" && fb != i18n.Base {
			note = "  (overrides " + fb + ")"
		}
		fmt.Fprintf(w, "%-8s %-28s %4d/%-4d %5.1f%%%s\n", info.Tag, info.NativeName, n, len(total), 100*float64(n)/float64(len(total)), note)
	}
}

func runCheck(w io.Writer) error {
	rep, err := check.Run(".")
	if err != nil {
		return err
	}
	for _, m := range rep.Warnings {
		fmt.Fprintln(w, "warning:", m)
	}
	for _, m := range rep.Errors {
		fmt.Fprintln(w, "error:", m)
	}
	fmt.Fprintf(w, "i18n-check: %d keys in en.json, %d errors, %d warnings\n", rep.Total, len(rep.Errors), len(rep.Warnings))
	if len(rep.Errors) > 0 {
		return fmt.Errorf("%d problems", len(rep.Errors))
	}
	return nil
}

// scaffold writes a new locale file: en.json's keys in their original order
// with the English text as a starting point, and a _meta block to fill in.
func scaffold(tag, name, native string) error {
	if !regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`).MatchString(tag) {
		return fmt.Errorf("%q is not a language tag such as fr or pt-BR", tag)
	}
	if name == "" {
		name = "TODO English name"
	}
	if native == "" {
		native = "TODO native name"
	}
	dst := filepath.Join(localeDir, tag+".json")
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	raw, err := os.ReadFile(filepath.Join(localeDir, "en.json"))
	if err != nil {
		return fmt.Errorf("run this from the repository root: %w", err)
	}
	keys, vals, err := orderedPairs(raw)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	meta, _ := json.MarshalIndent(map[string]any{
		"name": name, "native_name": native, "translators": []string{"your name"}, "fallback": "en",
	}, "  ", "  ")
	fmt.Fprintf(&out, "{\n  \"_meta\": %s,\n", meta)
	var prevGroup string
	first := true
	for i, k := range keys {
		if k == "_meta" {
			continue
		}
		group, _, _ := strings.Cut(k, ".")
		if !first && group != prevGroup {
			out.WriteString("\n")
		}
		prevGroup, first = group, false
		kj, _ := json.Marshal(k)
		vj, _ := json.Marshal(vals[i])
		sep := ","
		if i == len(keys)-1 {
			sep = ""
		}
		fmt.Fprintf(&out, "  %s: %s%s\n", kj, strings.ReplaceAll(string(vj), `&`, "&"), sep)
	}
	out.WriteString("}\n")
	if err := os.WriteFile(dst, out.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\nTranslate the values (not the keys), fill in _meta, then run: make i18n-check\n", dst)
	return nil
}

// orderedPairs reads a flat JSON object keeping key order; values that are not
// strings (the _meta object) come back empty.
func orderedPairs(raw []byte) (keys, vals []string, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err = dec.Token(); err != nil {
		return
	}
	for dec.More() {
		var kt json.Token
		if kt, err = dec.Token(); err != nil {
			return
		}
		var v json.RawMessage
		if err = dec.Decode(&v); err != nil {
			return
		}
		var s string
		_ = json.Unmarshal(v, &s)
		keys = append(keys, kt.(string))
		vals = append(vals, s)
	}
	return
}
