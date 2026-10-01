// Package check verifies that the translation keys used in Go code and the
// locale files agree. It backs `make i18n-check` and the Go test that runs in
// CI; see docs/TRANSLATING.md for the rules it enforces.
//
// It reads the Go sources with go/ast and recognises these call shapes:
//
//	x.T("key", ...)           x.Tn("key", n, ...)       (any receiver)
//	t(pd, "key", ...)         tn(pd, "key", n, ...)     (the web helpers)
//	i18n.Key("key")                                     (marks a key used indirectly)
//
// A key that is not a string literal cannot be checked, so it is an error
// unless the line carries an `i18n:dynamic` comment. Keys a dynamic lookup can
// return must then be registered with i18n.Key so they are still verified and
// counted as used.
package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

// Report is the outcome of Run. Errors fail CI; Warnings (keys a locale has
// not translated yet) are reported but do not.
type Report struct {
	Errors   []string
	Warnings []string
	// Missing counts keys absent from each non-base locale (they fall back).
	Missing map[string]int
	// Total is the number of keys in the base locale.
	Total int
}

var (
	placeholderRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	pluralSuffix  = []string{".zero", ".one", ".two", ".few", ".many", ".other"}
	// dynamicPrefixes are key families the i18n package itself builds at run
	// time (format.weekday.short.3 ...), so no Go call site names them.
	dynamicPrefixes = []string{"format."}
)

type usage struct {
	key    string
	plural bool
	where  string
}

// Run scans the Go files under root (the module root) and checks them against
// the embedded locales.
func Run(root string) (*Report, error) {
	uses, errs, err := scan(root)
	if err != nil {
		return nil, err
	}
	rep := &Report{Errors: errs, Missing: map[string]int{}}
	b := i18n.Default()
	en := b.Locale(i18n.Base)

	enKeys := map[string]bool{}
	for _, k := range en.Keys() {
		enKeys[k] = true
	}
	rep.Total = len(enKeys)

	used := map[string]bool{}
	for _, u := range uses {
		if u.plural {
			found := false
			for _, s := range pluralSuffix {
				if enKeys[u.key+s] {
					used[u.key+s] = true
					found = true
				}
			}
			if !found {
				rep.Errors = append(rep.Errors, fmt.Sprintf("%s: plural key %q has no variants in en.json", u.where, u.key))
			}
			continue
		}
		if !enKeys[u.key] {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: key %q is not in en.json", u.where, u.key))
			continue
		}
		used[u.key] = true
	}

	// Unused keys in the base locale.
	var unused []string
	for k := range enKeys {
		if used[k] || hasPrefix(k, dynamicPrefixes) {
			continue
		}
		unused = append(unused, k)
	}
	sort.Strings(unused)
	for _, k := range unused {
		rep.Errors = append(rep.Errors, fmt.Sprintf("en.json: key %q is never used in Go code", k))
	}

	rep.Errors = append(rep.Errors, pluralShape(en)...)
	for _, info := range b.Locales() {
		if info.Tag == i18n.Base {
			continue
		}
		loc := b.Locale(info.Tag)
		have := map[string]bool{}
		for _, k := range loc.Keys() {
			have[k] = true
			if !enKeys[k] && !pluralVariantOfEN(k, enKeys) {
				rep.Errors = append(rep.Errors, fmt.Sprintf("%s.json: key %q does not exist in en.json", info.Tag, k))
				continue
			}
			rep.Errors = append(rep.Errors, placeholderDiff(info.Tag, k, loc, en, enKeys)...)
		}
		rep.Errors = append(rep.Errors, pluralShape(loc)...)
		// A locale with no own keys (a regional variant such as de-AT) only
		// overrides; count against its fallback chain's coverage, not en's.
		if loc.Meta.Fallback != "" && loc.Meta.Fallback != i18n.Base {
			continue
		}
		n := 0
		for k := range enKeys {
			if !have[k] && !pluralCovered(k, have) {
				n++
			}
		}
		if n > 0 {
			rep.Missing[info.Tag] = n
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s.json: %d of %d keys missing (falls back to English)", info.Tag, n, rep.Total))
		}
	}
	sort.Strings(rep.Errors)
	sort.Strings(rep.Warnings)
	return rep, nil
}

func hasPrefix(k string, ps []string) bool {
	for _, p := range ps {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// pluralBase splits "items.other" into ("items", ".other").
func pluralBase(k string) (string, string, bool) {
	for _, s := range pluralSuffix {
		if strings.HasSuffix(k, s) {
			return strings.TrimSuffix(k, s), s, true
		}
	}
	return "", "", false
}

// pluralVariantOfEN: a locale may use plural categories English lacks (Russian
// "few"/"many"); they are valid when en has the base key's plural group.
func pluralVariantOfEN(k string, en map[string]bool) bool {
	base, _, ok := pluralBase(k)
	return ok && en[base+".other"]
}

// pluralCovered: an en plural variant the locale does not need (English "one"
// in Japanese) is not missing when the locale has the group's ".other".
func pluralCovered(k string, have map[string]bool) bool {
	base, _, ok := pluralBase(k)
	return ok && have[base+".other"]
}

// pluralShape: every plural group needs an ".other" form, the one lookup falls
// back to when a category has no variant.
func pluralShape(l *i18n.Locale) []string {
	var errs []string
	keys := l.Keys()
	groups := map[string]bool{}
	for _, k := range keys {
		if base, _, ok := pluralBase(k); ok {
			groups[base] = true
		}
	}
	for base := range groups {
		if _, ok := l.Message(base + ".other"); !ok {
			errs = append(errs, fmt.Sprintf("%s.json: plural key %q needs a %q variant", l.Tag, base, base+".other"))
		}
	}
	return errs
}

func placeholders(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
		out[m[1]] = true
	}
	return out
}

// placeholderDiff compares a translation's {placeholders} with English: a
// translation must not invent or drop one. Plural variants compare against
// the union over the English group, since "one" forms may omit {n}.
func placeholderDiff(tag, key string, loc, en *i18n.Locale, enKeys map[string]bool) []string {
	got, _ := loc.Message(key)
	var want map[string]bool
	if base, _, ok := pluralBase(key); ok && enKeys[base+".other"] {
		want = map[string]bool{}
		for _, s := range pluralSuffix {
			if m, ok := en.Message(base + s); ok {
				for p := range placeholders(m) {
					want[p] = true
				}
			}
		}
		gotP := placeholders(got)
		var errs []string
		for p := range gotP {
			if !want[p] {
				errs = append(errs, fmt.Sprintf("%s.json: %q uses {%s}, which en.json does not", tag, key, p))
			}
		}
		return errs
	}
	m, ok := en.Message(key)
	if !ok {
		return nil
	}
	want = placeholders(m)
	gotP := placeholders(got)
	var errs []string
	for p := range want {
		if !gotP[p] {
			errs = append(errs, fmt.Sprintf("%s.json: %q is missing {%s}", tag, key, p))
		}
	}
	for p := range gotP {
		if !want[p] {
			errs = append(errs, fmt.Sprintf("%s.json: %q has {%s}, which en.json does not", tag, key, p))
		}
	}
	return errs
}

// scan collects key usages from the non-test Go files under root.
func scan(root string) ([]usage, []string, error) {
	var uses []usage
	var errs []string
	skipDir := filepath.Join(root, "internal", "i18n")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "pb_data":
				return filepath.SkipDir
			}
			if path == skipDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		u, e, err := scanFile(root, path)
		if err != nil {
			return err
		}
		uses = append(uses, u...)
		errs = append(errs, e...)
		return nil
	})
	return uses, errs, err
}

func scanFile(root, path string) ([]usage, []string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	rel, _ := filepath.Rel(root, path)

	// Lines that opt out with an i18n:dynamic comment.
	dynamic := map[int]bool{}
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.Contains(c.Text, "i18n:dynamic") {
				ln := fset.Position(c.Pos()).Line
				dynamic[ln], dynamic[ln+1] = true, true
			}
		}
	}

	var uses []usage
	var errs []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		keyIdx, plural := keyArg(call)
		if keyIdx < 0 || keyIdx >= len(call.Args) {
			return true
		}
		pos := fset.Position(call.Pos())
		where := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line)
		arg := call.Args[keyIdx]
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			if !dynamic[pos.Line] {
				errs = append(errs, fmt.Sprintf("%s: translation key is not a string literal (use a literal, or i18n.Key for a table and mark the lookup // i18n:dynamic)", where))
			}
			return true
		}
		key, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		uses = append(uses, usage{key: key, plural: plural, where: where})
		return true
	})
	return uses, errs, nil
}

// keyArg returns the index of the key argument for the recognised call shapes,
// or -1. plural is true for Tn/tn, whose key names a group of variants.
func keyArg(call *ast.CallExpr) (idx int, plural bool) {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		switch fn.Sel.Name {
		case "T":
			if x, ok := fn.X.(*ast.Ident); ok && x.Name == "testing" {
				return -1, false
			}
			return 0, false
		case "Tn":
			return 0, true
		case "Key":
			if x, ok := fn.X.(*ast.Ident); ok && x.Name == "i18n" {
				return 0, false
			}
		}
	case *ast.Ident:
		switch fn.Name {
		case "t":
			return 1, false
		case "tn":
			return 1, true
		}
	}
	return -1, false
}
