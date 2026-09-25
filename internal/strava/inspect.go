package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"
)

// ElementInfo describes one form element for diagnostics.
type ElementInfo struct {
	Tag   string `json:"tag"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"` // aria-label or placeholder
	Text  string `json:"text"`  // visible text of buttons
}

// Report is the result of a dry run against the edit page. Nothing is saved.
type Report struct {
	RequestedURL        string
	FinalURL            string
	Title               string
	LoggedIn            bool
	DescriptionSelector string // empty when no candidate matched
	Description         string
	SaveMethod          string // "form-button", "selector", "form-submit" or empty
	Textareas           []ElementInfo
	Buttons             []ElementInfo
	FileInputs          []ElementInfo // where a photo could be attached
	PhotoSelector       string        // empty when no Photo candidate matched
	HTML                string        // set only when includeHTML is true
}

// Inspect opens the edit page and reports what it finds, without changing anything.
// A missing selector is not an error: the report lists the elements that exist, so
// you can pick working selectors. When includeHTML is true, the page source is included.
func (w *Writer) Inspect(ctx context.Context, stravaID string, includeHTML bool) (*Report, error) {
	if !idRe.MatchString(stravaID) {
		return nil, fmt.Errorf("strava: invalid activity id %q", stravaID)
	}
	editURL := w.cfg.BaseURL + "/activities/" + stravaID + "/edit"
	rep := &Report{RequestedURL: editURL}

	err := w.withBrowser(ctx, func(ctx context.Context) error {
		loc, err := w.navigate(ctx, editURL)
		if err != nil {
			return err
		}
		rep.FinalURL = loc
		rep.LoggedIn = !isLoginURL(loc)
		if err := chromedp.Run(ctx, chromedp.Title(&rep.Title)); err != nil {
			return err
		}
		if !rep.LoggedIn {
			return nil
		}

		if sel, err := w.locate(ctx, "description", loc, w.cfg.Selectors.Description); err == nil {
			rep.DescriptionSelector = sel
			rep.Description, _ = evalString(ctx, `document.querySelector(`+jsStr(sel)+`).value`)
			rep.SaveMethod, _ = evalString(ctx, probeSaveJS(sel, w.cfg.Selectors.Save))
		} else if _, ok := err.(*SelectorError); !ok {
			return err
		}

		raw, err := evalString(ctx, inventoryJS)
		if err != nil {
			return err
		}
		var inv struct {
			Textareas []ElementInfo `json:"textareas"`
			Buttons   []ElementInfo `json:"buttons"`
			Files     []ElementInfo `json:"files"`
		}
		if err := json.Unmarshal([]byte(raw), &inv); err == nil {
			rep.Textareas, rep.Buttons, rep.FileInputs = inv.Textareas, inv.Buttons, inv.Files
			rep.PhotoSelector, _ = evalString(ctx, `(function(c){for(const s of c){try{if(document.querySelector(s))return s}catch(e){}}return ""})(`+jsJSON(w.cfg.Selectors.Photo)+`)`)
		}
		if includeHTML {
			rep.HTML, _ = evalString(ctx, `document.documentElement.outerHTML`)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rep, nil
}

// String formats the report for a terminal.
func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "requested:  %s\nfinal URL:  %s\ntitle:      %s\nlogged in:  %v\n", r.RequestedURL, r.FinalURL, r.Title, r.LoggedIn)
	if !r.LoggedIn {
		b.WriteString("\nStrava redirected to the login page: the stored cookies are not valid.\n")
		return b.String()
	}
	if r.DescriptionSelector == "" {
		b.WriteString("description: NOT FOUND with the configured selectors\n")
	} else {
		fmt.Fprintf(&b, "description: matched %s (%d characters)\n", r.DescriptionSelector, len(r.Description))
		fmt.Fprintf(&b, "save:        %s\n", orNone(r.SaveMethod))
	}
	list := func(name string, els []ElementInfo) {
		fmt.Fprintf(&b, "\n%s (%d):\n", name, len(els))
		for _, e := range els {
			fmt.Fprintf(&b, "  <%s id=%q name=%q type=%q label=%q text=%q>\n", e.Tag, e.ID, e.Name, e.Type, e.Label, e.Text)
		}
	}
	list("textareas", r.Textareas)
	list("buttons", r.Buttons)
	list("file inputs", r.FileInputs)
	fmt.Fprintf(&b, "photo:       %s\n", photoOrNone(r.PhotoSelector))
	return b.String()
}

func photoOrNone(s string) string {
	if s == "" {
		return "NOT FOUND with the configured selectors (chart upload would fail)"
	}
	return "matched " + s
}

func orNone(s string) string {
	if s == "" {
		return "NOT FOUND (no form submit button and no Save selector matched)"
	}
	return s
}

const inventoryJS = `JSON.stringify((function(){
  const info=e=>({tag:e.tagName.toLowerCase(),id:e.id||'',name:e.getAttribute('name')||'',type:e.getAttribute('type')||'',
    label:e.getAttribute('aria-label')||e.getAttribute('placeholder')||'',text:(e.innerText||e.value||'').trim().slice(0,40)});
  return {textareas:[...document.querySelectorAll('textarea')].map(info),
          buttons:[...document.querySelectorAll('button,input[type=submit],input[type=button]')].map(info),
          files:[...document.querySelectorAll('input[type=file]')].map(info)};
})())`

// probeSaveJS mirrors the lookup in save() without clicking anything.
func probeSaveJS(descSel string, saveCands []string) string {
	return `(function(sel,cands){
	  const el=document.querySelector(sel); const f=el.form||el.closest('form');
	  if(f&&f.querySelector('button[type=submit],input[type=submit]'))return 'form-button';
	  for(const s of cands){if(document.querySelector(s))return 'selector'}
	  if(f)return 'form-submit';
	  return ''})(` + jsStr(descSel) + `,` + jsJSON(saveCands) + `)`
}
