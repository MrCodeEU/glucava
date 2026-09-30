package digest

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/i18n"
	"github.com/MrCodeEU/glucava/internal/jobs"
	"github.com/MrCodeEU/glucava/internal/notify"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
)

// HealthInput is what the monthly health report is built from.
type HealthInput struct {
	Activities []jobs.Activity
	Events     []store.EventRow
	Samples    []stats.Sample
	From, To   time.Time // the month covered: [From, To)
	Build      string
}

// coverage is the share of hours in [from, to) that have at least one
// reading. Counting hours instead of readings keeps it right for sensors with
// different reading intervals.
func coverage(samples []stats.Sample, from, to time.Time) float64 {
	hours := int(to.Sub(from).Hours())
	if hours <= 0 {
		return 0
	}
	seen := map[int]bool{}
	for _, s := range samples {
		if !s.Time.Before(from) && s.Time.Before(to) {
			seen[int(s.Time.Sub(from).Hours())] = true
		}
	}
	return 100 * float64(len(seen)) / float64(hours)
}

// HealthMessage builds the monthly report: is glucava still doing its job?
func HealthMessage(tr *i18n.Translator, in HealthInput, loc *time.Location) notify.Message {
	var done, failed int
	var lastDone time.Time
	for _, a := range in.Activities {
		if a.Start.Before(in.From) || !a.Start.Before(in.To) {
			continue
		}
		switch a.Status {
		case jobs.StatusDone:
			done++
			if a.Start.After(lastDone) {
				lastDone = a.Start
			}
		case jobs.StatusFailed:
			failed++
		}
	}
	byType := map[string]int{}
	for _, e := range in.Events {
		if e.Created.Before(in.From) || !e.Created.Before(in.To) {
			continue
		}
		byType[e.Type]++
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)

	cov := coverage(in.Samples, in.From, in.To)
	facts := []notify.Fact{
		{Label: tr.T("email.health.done"), Value: tr.Int(done)},
		{Label: tr.T("email.health.failed"), Value: tr.Int(failed)},
		{Label: tr.T("email.health.coverage"), Value: pct(tr, cov)},
	}
	if !lastDone.IsZero() {
		facts = append(facts, notify.Fact{Label: tr.T("email.health.latest"), Value: tr.Date(lastDone.In(loc), false) + ", " + tr.Time(lastDone.In(loc))})
	}
	for _, t := range types {
		facts = append(facts, notify.Fact{Label: tr.T("email.health.alert", "title", notify.Title(tr, t)), Value: tr.Int(byType[t])})
	}
	if in.Build != "" {
		facts = append(facts, notify.Fact{Label: tr.T("email.health.version"), Value: in.Build})
	}

	var problems []string
	if failed > 0 {
		problems = append(problems, tr.Tn("email.health.problem.failed", failed))
	}
	if cov < 90 {
		problems = append(problems, tr.T("email.health.problem.coverage", "pct", pct(tr, cov)))
	}
	sev, body := "info", tr.T("email.health.ok")
	if len(problems) > 0 {
		sev, body = "warning", tr.T("email.health.problems", "list", tr.List(problems))
	}
	return notify.Message{
		Type: notify.TypeHealthReport, Severity: sev, Body: body, Facts: facts, Time: time.Now(),
		Title: tr.T("email.health.title", "month", tr.Month(in.From.In(loc).Month(), true), "year", fmt.Sprint(in.From.In(loc).Year())),
	}
}

// MonthStart returns the 1st of t's month at 08:00 in loc.
func MonthStart(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), 1, 8, 0, 0, 0, loc)
}

// HealthStore is what the monthly scheduler needs.
type HealthStore interface {
	ListActivities(ctx context.Context, limit int) ([]jobs.Activity, error)
	ListEvents(ctx context.Context, limit int) ([]store.EventRow, error)
	LoadSamplesAny(ctx context.Context, from, to time.Time) ([]stats.Sample, error)
	HealthLast() time.Time
	SetHealthLast(t time.Time) error
}

// Health sends the monthly health report once per month, on the 1st from
// 08:00 (or later that month if the server was down), when enabled.
type Health struct {
	Store   HealthStore
	Enabled func() bool
	Loc     func() *time.Location
	Build   string
	Send    func(ctx context.Context, m notify.Message) error
	Now     func() time.Time
	Every   time.Duration // default 30 minutes
	// Tr gives the installation language at send time; nil means English.
	Tr notify.Translator
}

func (h *Health) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Once sends the report if it is due and returns whether it did.
func (h *Health) Once(ctx context.Context) (bool, error) {
	if !h.Enabled() {
		return false, nil
	}
	loc := h.Loc()
	now := h.now()
	due := MonthStart(now, loc)
	if now.Before(due) || !h.Store.HealthLast().Before(due) {
		return false, nil
	}
	to := time.Date(due.Year(), due.Month(), 1, 0, 0, 0, 0, loc)
	from := to.AddDate(0, -1, 0)

	acts, err := h.Store.ListActivities(ctx, 1000)
	if err != nil {
		return false, err
	}
	events, err := h.Store.ListEvents(ctx, 1000)
	if err != nil {
		return false, err
	}
	samples, err := h.Store.LoadSamplesAny(ctx, from, to)
	if err != nil {
		return false, err
	}
	msg := HealthMessage(h.Tr.Get(), HealthInput{Activities: acts, Events: events, Samples: samples, From: from, To: to, Build: h.Build}, loc)
	if err := h.Send(ctx, msg); err != nil {
		return false, err
	}
	return true, h.Store.SetHealthLast(now)
}

// Run checks until ctx is cancelled.
func (h *Health) Run(ctx context.Context) {
	every := h.Every
	if every <= 0 {
		every = 30 * time.Minute
	}
	for {
		if _, err := h.Once(ctx); err != nil {
			slog.Error("health report", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
