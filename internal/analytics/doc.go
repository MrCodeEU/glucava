// Package analytics computes the clinical and pattern metrics behind the
// Overview, activity and report pages: five-band time in range, AGP
// percentile curves, the Glycemia Risk Index, CGM coverage, glucose episodes
// and calendar/day-part/weekday aggregates.
//
// Everything here is pure: functions take []stats.Sample (mg/dL) plus the
// thresholds and time zone to use and return plain values, so they can be
// unit-tested without a store and their results can be embedded in JSON for
// the charts. Input slices are never modified. Functions that need time order
// accept unsorted input and sort a copy only when it is not already ordered.
//
// Threshold semantics match stats.Summarize: a reading below Low is "low"
// (and below VeryLow "very low"), a reading above High is "high" (above
// VeryHigh "very high"); Low and High themselves are in range.
package analytics
