package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Kinds of a manual verdict on a span of glucose readings.
const (
	// MarkArtifact means the person marked the span as not real (a sensor
	// artifact).
	MarkArtifact = "artifact"
	// MarkReal means the person confirmed the span was real, overriding a
	// suspected artifact.
	MarkReal = "real"
)

// ArtifactMark is one manual verdict. Marks are append-only: the newest one
// over a span wins, so changing one's mind is just another mark.
type ArtifactMark struct {
	Start, End time.Time
	Kind       string // MarkArtifact or MarkReal
	Created    time.Time
}

// AddArtifactMark records a verdict on [start, end].
func (s *PB) AddArtifactMark(_ context.Context, start, end time.Time, kind string) error {
	if kind != MarkArtifact && kind != MarkReal {
		return fmt.Errorf("store: unknown mark kind %q", kind)
	}
	if end.Before(start) {
		return fmt.Errorf("store: mark ends before it starts")
	}
	col, err := s.App.FindCollectionByNameOrId("artifact_marks")
	if err != nil {
		return err
	}
	var st, en types.DateTime
	if err := st.Scan(start.UTC()); err != nil {
		return err
	}
	if err := en.Scan(end.UTC()); err != nil {
		return err
	}
	r := core.NewRecord(col)
	r.Set("start", st)
	r.Set("end", en)
	r.Set("kind", kind)
	return s.App.Save(r)
}

// ArtifactMarks returns the verdicts that overlap [from, to], oldest first.
func (s *PB) ArtifactMarks(_ context.Context, from, to time.Time) ([]ArtifactMark, error) {
	rows, err := s.App.DB().NewQuery(`SELECT "start", "end", kind, created FROM artifact_marks
		WHERE "end" >= {:from} AND "start" <= {:to} ORDER BY created, rowid`).
		Bind(map[string]any{"from": pbTime(from), "to": pbTime(to)}).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ArtifactMark
	for rows.Next() {
		var st, en, kind, created sql.NullString
		if err := rows.Scan(&st, &en, &kind, &created); err != nil {
			return nil, err
		}
		m := ArtifactMark{Kind: kind.String}
		m.Start, _ = time.Parse(pbStoredTime, st.String)
		m.End, _ = time.Parse(pbStoredTime, en.String)
		m.Created, _ = time.Parse(pbStoredTime, created.String)
		out = append(out, m)
	}
	return out, rows.Err()
}
