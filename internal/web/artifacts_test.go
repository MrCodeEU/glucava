package web

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MrCodeEU/glucava/internal/analytics"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/store"
)

func TestActivityPageNoticesOverlappingArtifact(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.srv.Now = func() time.Time { return testNow }
	c := e.login(t)
	seedOverview(t, e)
	act := &jobs.Activity{StravaID: "9002", Name: "Night Walk", Sport: "Walk", Start: time.Date(2026, 9, 28, 2, 30, 0, 0, time.UTC),
		Duration: 45 * time.Minute, Status: jobs.StatusDone}
	if err := e.srv.Store.SaveActivity(context.Background(), act); err != nil {
		t.Fatal(err)
	}
	if body := e.get(t, "/activity/9002", c).Body.String(); !strings.Contains(body, "Possible sensor artifact") {
		t.Error("activity page has no artifact notice")
	}
	if body := e.get(t, "/activity/9001", c).Body.String(); strings.Contains(body, "Possible sensor artifact") {
		t.Error("notice shown for an activity without an artifact")
	}
}

func TestOverviewFlagsSuspectedArtifact(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.srv.Now = func() time.Time { return testNow }
	c := e.login(t)
	seedOverview(t, e) // its 03:00 low drops from ~137 to 55 in one reading and returns

	body := e.get(t, "/stats?range=7d", c).Body.String()
	for _, want := range []string{"possible compression low", "as recorded and", "without the 1 suspected sensor artifact", "/actions/artifact-mark?"} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if strings.Contains(body, "Left out as suspected sensor artifacts") {
		t.Error("flag mode must not list left-out artifacts")
	}

	cfg, err := e.srv.Store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ArtifactMode = store.ArtifactExclude
	if err := e.srv.Store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	body = e.get(t, "/stats?range=7d", c).Body.String()
	for _, want := range []string{"Left out as suspected sensor artifacts", "It was real", "left out"} {
		if !strings.Contains(body, want) {
			t.Errorf("exclude mode is missing %q", want)
		}
	}
}

func TestArtifactMarkActionAndCacheKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.srv.Now = func() time.Time { return testNow }
	c := e.login(t)
	seedOverview(t, e)

	from := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)
	q := url.Values{"start": {strconv.FormatInt(from.Unix(), 10)}, "end": {strconv.FormatInt(from.Add(30*time.Minute).Unix(), 10)}, "kind": {"real"}}
	before := e.get(t, "/stats?range=7d", c).Body.String()
	if !strings.Contains(before, "possible compression low") {
		t.Fatal("precondition: a suspected artifact is flagged")
	}
	if w := e.action("/actions/artifact-mark?"+q.Encode(), "{}", c, nil); !strings.Contains(w.Body.String(), "Marked as real") {
		t.Fatalf("mark response: %s", w.Body)
	}
	marks, err := e.srv.Store.ArtifactMarks(context.Background(), from.Add(-time.Hour), from.Add(time.Hour))
	if err != nil || len(marks) != 1 || marks[0].Kind != store.MarkReal {
		t.Fatalf("marks = %+v, %v", marks, err)
	}
	after := e.get(t, "/stats?range=7d", c).Body.String() // a new mark must change the cached model
	if strings.Contains(after, "possible compression low") {
		t.Error("a low marked as real is still flagged")
	}
}

func TestArtifactMarkActionRejectsBadInput(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	for name, q := range map[string]string{
		"bad kind":  "start=1000&end=2000&kind=nope",
		"reversed":  "start=2000&end=1000&kind=artifact",
		"too long":  "start=1000&end=1000000&kind=artifact",
		"not a num": "start=x&end=2000&kind=artifact",
	} {
		w := e.action("/actions/artifact-mark?"+q, "{}", c, nil)
		if strings.Contains(w.Body.String(), "Marked as") {
			t.Errorf("%s: accepted", name)
		}
	}
	if w := e.action("/actions/artifact-mark?start=1000&end=2000&kind=artifact", "{}", nil, nil); w.Code != 303 && w.Code != 401 && w.Code != 403 {
		t.Errorf("unauthenticated = %d", w.Code)
	}
	marks, _ := e.srv.Store.ArtifactMarks(context.Background(), time.Unix(0, 0), time.Unix(1e6, 0))
	if len(marks) != 0 {
		t.Errorf("bad requests stored marks: %+v", marks)
	}
}

func TestArtifactModeSettingRoundTrip(t *testing.T) {
	t.Parallel()
	cfg := store.Config{ArtifactMode: store.ArtifactExclude}
	raw, _ := json.Marshal(overviewSignals(cfg))
	var v settingsSignals
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.ArtifactMode != store.ArtifactExclude {
		t.Errorf("signal = %q", v.ArtifactMode)
	}
	if got := (store.Config{}).ArtifactsMode(); got != store.ArtifactFlagged {
		t.Errorf("default mode = %q", got)
	}
}

func TestArtifactLabelAndFor(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 30, 3, 0, 0, 0, time.UTC)
	m := &overviewModel{Artifacts: []analytics.Artifact{{Kind: analytics.ArtifactDip, Start: at, End: at.Add(10 * time.Minute)}}}
	e := analytics.Episode{Kind: analytics.KindLow, Start: at.Add(5 * time.Minute), End: at.Add(20 * time.Minute)}
	a := artifactFor(m, e)
	if a == nil || artifactLabel(*a) != "possible sensor dip" {
		t.Fatalf("artifactFor = %+v", a)
	}
	if artifactFor(m, analytics.Episode{Kind: analytics.KindHigh, Start: at, End: at.Add(time.Hour)}) != nil {
		t.Error("highs are never artifacts")
	}
	if artifactLabel(analytics.Artifact{Marked: true}) != "marked not real" {
		t.Error("marked label")
	}
}
