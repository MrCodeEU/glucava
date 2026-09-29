package taskerprofile

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// TestBuildShape reproduces, structurally, the profile a real user built by
// hand and confirmed working: a Notification event (code 461) on the Strava
// app, unfiltered, wired to a task with one HTTP Request action (code 339)
// that POSTs to /api/trigger with the bearer token.
func TestBuildShape(t *testing.T) {
	out := string(Build("https://glucava.example.com", "gst_abc123"))

	dec := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("output is not well-formed XML: %v", err)
		}
	}

	for _, want := range []string{
		"<code>461</code>",            // Notification event
		"<appPkg>com.strava</appPkg>", // owner app: Strava
		"<code>339</code>",            // HTTP Request action
		"<Str sr=\"arg2\" ve=\"3\">https://glucava.example.com/api/trigger</Str>",
		"<Str sr=\"arg3\" ve=\"3\">Authorization:Bearer gst_abc123</Str>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}

	// Unfiltered: the notification title/text filter args (arg1..arg6) are
	// empty, self-closed elements, never a hardcoded/localized string.
	if !strings.Contains(out, `<Str sr="arg1" ve="3"/>`) {
		t.Errorf("expected an empty, self-closed arg1 (no title filter), got:\n%s", out)
	}
}

func TestBuildStripsTrailingSlashFromBaseURL(t *testing.T) {
	out := string(Build("https://glucava.example.com/", "gst_abc123"))
	if !strings.Contains(out, "https://glucava.example.com/api/trigger") {
		t.Errorf("trailing slash not collapsed, got:\n%s", out)
	}
	if strings.Contains(out, "//api/trigger") {
		t.Errorf("double slash in URL, got:\n%s", out)
	}
}

func TestBuildEscapesXML(t *testing.T) {
	out := string(Build("https://glucava.example.com", `gst_a"b&c`))
	if strings.Contains(out, `"b&c`) {
		t.Errorf("unescaped XML special characters leaked into output:\n%s", out)
	}
}
