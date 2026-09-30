package analytics

import (
	"math"
	"sort"
	"time"

	"github.com/MrCodeEU/glucava/internal/stats"
)

// Block is the aggregate of a group of readings.
type Block struct {
	Count  int     `json:"count"`
	Avg    float64 `json:"avg"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	StdDev float64 `json:"stdDev"`
	CV     float64 `json:"cv"` // percent
	TIR    TIR5    `json:"tir"`
}

// Aggregate summarizes readings; the zero Block for none. Population
// standard deviation, like stats.Summarize.
func Aggregate(samples []stats.Sample, th Thresholds) Block {
	if len(samples) == 0 {
		return Block{}
	}
	var acc blockAcc
	for _, s := range samples {
		acc.add(s.Value, th)
	}
	return acc.block()
}

// blockAcc accumulates a Block in one pass (Welford's variance).
type blockAcc struct {
	n        int
	mean, m2 float64
	min, max float64
	bands    [5]int
}

func (a *blockAcc) add(v float64, th Thresholds) {
	a.n++
	if a.n == 1 {
		a.min, a.max = v, v
	} else {
		a.min, a.max = math.Min(a.min, v), math.Max(a.max, v)
	}
	d := v - a.mean
	a.mean += d / float64(a.n)
	a.m2 += d * (v - a.mean)
	a.bands[th.BandOf(v)]++
}

func (a *blockAcc) block() Block {
	if a.n == 0 {
		return Block{}
	}
	b := Block{Count: a.n, Avg: a.mean, Min: a.min, Max: a.max, StdDev: math.Sqrt(a.m2 / float64(a.n)),
		TIR: tir5FromCounts(a.bands, a.n)}
	if a.mean > 0 {
		b.CV = b.StdDev / a.mean * 100
	}
	return b
}

// DayPart is a named slice of the local day.
type DayPart struct {
	Name     string `json:"name"`
	FromHour int    `json:"fromHour"` // inclusive
	ToHour   int    `json:"toHour"`   // exclusive
	Block
}

// DayPartBounds are the fixed parts: night 00-06, morning 06-12,
// afternoon 12-18, evening 18-24 in local time.
var DayPartBounds = [4]struct {
	Name     string
	From, To int
}{{"Night", 0, 6}, {"Morning", 6, 12}, {"Afternoon", 12, 18}, {"Evening", 18, 24}}

// ComputeDayParts aggregates readings by local time of day. A nil loc means UTC.
func ComputeDayParts(samples []stats.Sample, th Thresholds, loc *time.Location) [4]DayPart {
	if loc == nil {
		loc = time.UTC
	}
	var acc [4]blockAcc
	for _, s := range samples {
		acc[s.Time.In(loc).Hour()/6].add(s.Value, th)
	}
	var out [4]DayPart
	for i, b := range DayPartBounds {
		out[i] = DayPart{Name: b.Name, FromHour: b.From, ToHour: b.To, Block: acc[i].block()}
	}
	return out
}

// Cell is one weekday/hour heatmap cell. Percentages are of the cell's
// readings; all zero when Count == 0.
type Cell struct {
	Count   int     `json:"count"`
	Mean    float64 `json:"mean"`
	InRange float64 `json:"inRange"`
	Below   float64 `json:"below"`
	Above   float64 `json:"above"`
}

// WeekdayHour is a 7x24 grid indexed [time.Weekday][local hour]; Weekday
// numbering is Go's (Sunday = 0), so callers wanting Monday first map it.
type WeekdayHour [7][24]Cell

// ComputeWeekdayHour fills the heatmap grid by local time. A nil loc means UTC.
func ComputeWeekdayHour(samples []stats.Sample, th Thresholds, loc *time.Location) WeekdayHour {
	if loc == nil {
		loc = time.UTC
	}
	type acc struct {
		n                int
		sum              float64
		in, below, above int
	}
	var a [7][24]acc
	for _, s := range samples {
		t := s.Time.In(loc)
		c := &a[t.Weekday()][t.Hour()]
		c.n++
		c.sum += s.Value
		switch th.BandOf(s.Value) {
		case BandVeryLow, BandLow:
			c.below++
		case BandInRange:
			c.in++
		default:
			c.above++
		}
	}
	var out WeekdayHour
	for d := range a {
		for h, c := range a[d] {
			if c.n == 0 {
				continue
			}
			f := 100 / float64(c.n)
			out[d][h] = Cell{Count: c.n, Mean: c.sum / float64(c.n), InRange: float64(c.in) * f,
				Below: float64(c.below) * f, Above: float64(c.above) * f}
		}
	}
	return out
}

// Day is one local calendar day's aggregate.
type Day struct {
	Date time.Time `json:"date"` // local midnight
	Block
}

// ComputeDaily groups readings by local calendar day (never by 24h
// truncation, so DST days of 23 or 25 hours stay whole days), oldest first.
// Days without readings are omitted. A nil loc means UTC.
func ComputeDaily(samples []stats.Sample, th Thresholds, loc *time.Location) []Day {
	if loc == nil {
		loc = time.UTC
	}
	acc := map[time.Time]*blockAcc{}
	for _, s := range samples {
		t := s.Time.In(loc)
		y, m, d := t.Date()
		day := time.Date(y, m, d, 0, 0, 0, 0, loc)
		a := acc[day]
		if a == nil {
			a = &blockAcc{}
			acc[day] = a
		}
		a.add(s.Value, th)
	}
	out := make([]Day, 0, len(acc))
	for day, a := range acc {
		out = append(out, Day{Date: day, Block: a.block()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}
