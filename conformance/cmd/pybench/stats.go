package main

import (
	"sort"
	"time"
)

// Stats summarizes a series of repeated timing runs for one
// implementation/benchmark pair. Min is reported because process-timing
// noise (scheduler jitter, page faults, GC pauses) is one-sided: it can only
// slow a run down, never speed it up, so the fastest observed run is the
// closest estimate of true cost. Median is reported alongside as a
// noise-resistant central estimate.
type Stats struct {
	Min    time.Duration
	Median time.Duration
	N      int
}

// computeStats reduces durations to Stats. It panics on an empty input,
// since every caller only invokes it after at least one successful timed
// run; an empty slice there is a caller bug, not a reportable condition.
func computeStats(durations []time.Duration) Stats {
	if len(durations) == 0 {
		panic("pybench: computeStats called with no durations")
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return Stats{
		Min:    sorted[0],
		Median: median(sorted),
		N:      len(sorted),
	}
}

// median returns the middle value of sorted (already ascending), averaging
// the two central values for an even-length input.
func median(sorted []time.Duration) time.Duration {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// minusStartup subtracts startup from d, floored at zero so a noisy run that
// happened to measure faster than the implementation's own startup baseline
// is reported as "no measurable workload cost" rather than a negative
// duration.
func minusStartup(d, startup time.Duration) time.Duration {
	if d <= startup {
		return 0
	}
	return d - startup
}
