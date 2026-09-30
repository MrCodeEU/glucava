package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"testing"
)

// convertedFiles lists the files whose user-visible strings all go through the
// translator. The test below fails when one of them gains a hard-coded English
// text again. A nil function list means the whole file; otherwise only those
// functions are checked (files still being converted). Add a file here in the
// same change that converts it.
var convertedFiles = map[string][]string{
	"layout.go":      nil,
	"events_i18n.go": nil,
	"i18n.go":        nil,
	"auth.go":        nil,
	"handlers.go":    nil,
	"account.go":     nil,
	"server.go":      nil,
	"report.go":      nil,
	"pages.go":       {"EventsPage"},
	"components.go":  {"StatusBadgeT", "SeverityBadgeT", "ConfirmDialogT", "fmtWhenT"},
}

// english is what a hard-coded UI sentence or label looks like: a capitalised
// word followed by lower-case letters ("Sign in", "Nothing has gone wrong.").
var english = regexp.MustCompile(`^[A-Z][a-z]`)

// attrsWithText are attributes whose value a user reads or hears.
var attrsWithText = map[string]bool{"aria-label": true, "title": true, "placeholder": true, "alt": true}

func TestConvertedFilesHaveNoHardCodedText(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	for file, funcs := range convertedFiles {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		only := map[string]bool{}
		for _, fn := range funcs {
			only[fn] = true
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && len(only) > 0 && !only[fd.Name.Name] {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, lit := range visibleLiterals(call) {
					s, err := strconv.Unquote(lit.Value)
					if err == nil && english.MatchString(s) {
						t.Errorf("%s: hard-coded text %s: add a key to en.json and use t(pd, ...) or tr.T(...)", fset.Position(lit.Pos()), lit.Value)
					}
				}
				return true
			})
		}
	}
}

// visibleLiterals returns the string literals in call that end up as text in
// the page: g.Text("..."), g.Attr("aria-label", "...") style attributes, toast
// messages and http.Error bodies.
func visibleLiterals(call *ast.CallExpr) []*ast.BasicLit {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	str := func(e ast.Expr) *ast.BasicLit {
		if l, ok := e.(*ast.BasicLit); ok && l.Kind == token.STRING {
			return l
		}
		return nil
	}
	// A toast message or an http.Error body is read by the person too.
	switch {
	case sel.Sel.Name == "toast" && len(call.Args) == 3:
		if l := str(call.Args[2]); l != nil {
			return []*ast.BasicLit{l}
		}
	case sel.Sel.Name == "Error" && len(call.Args) == 3:
		if l := str(call.Args[1]); l != nil {
			return []*ast.BasicLit{l}
		}
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || x.Name != "g" {
		return nil
	}
	switch sel.Sel.Name {
	case "Text":
		if len(call.Args) == 1 {
			if l := str(call.Args[0]); l != nil {
				return []*ast.BasicLit{l}
			}
		}
	case "Attr":
		if len(call.Args) == 2 {
			if name := str(call.Args[0]); name != nil {
				if n, _ := strconv.Unquote(name.Value); attrsWithText[n] {
					if l := str(call.Args[1]); l != nil {
						return []*ast.BasicLit{l}
					}
				}
			}
		}
	}
	return nil
}

func TestLintCatchesHardCodedText(t *testing.T) {
	t.Parallel()
	src := `package x
func f() { _ = g.Text("Sign in"); _ = g.Text(tr.T("a.b")); _ = g.Text("glucava"); _ = g.Attr("aria-label", "Main") }`
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			for _, lit := range visibleLiterals(call) {
				if s, _ := strconv.Unquote(lit.Value); english.MatchString(s) {
					n++
				}
			}
		}
		return true
	})
	if n != 2 {
		t.Errorf("flagged %d literals, want 2 (Sign in, Main)", n)
	}
}
