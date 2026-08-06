package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/siyul-park/minipy/conformance"
)

// startupSource is the trivial program timed once per implementation to
// establish each interpreter's fixed process-start/parse/init cost, so a
// benchmark's own wall-clock time can be reported both raw and with that
// fixed cost subtracted out. It is valid under every implementation this
// tool drives, annotation-free so gpython needs no stripped copy for it.
const startupSource = "x = 1\n"

// buildMinipy compiles cmd/minipy once into a temporary directory outside
// the repository and returns its path plus a cleanup func that removes the
// temporary directory. Every implementation is invoked as a plain resolved
// binary (never `go run`, whose build/cache-check overhead would
// contaminate the startup timing this tool exists to measure), and minipy is
// no exception: this is the one build step that makes that true for it too.
func buildMinipy(ctx context.Context, repoRoot string) (bin string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "pybench-minipy-")
	if err != nil {
		return "", nil, fmt.Errorf("pybench: create temp build dir: %w", err)
	}
	cleanup = func() { os.RemoveAll(dir) }

	bin = filepath.Join(dir, "minipy")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/minipy")
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("pybench: build minipy: %w: %s", err, stderr.String())
	}
	return bin, cleanup, nil
}

// prepareStrippedCopies writes an annotation-stripped copy of every case's
// source into a temporary directory (see stripAnnotations), for gpython to
// run instead of the original. It returns a lookup from case name to
// stripped path and a cleanup func.
func prepareStrippedCopies(cases []conformance.Case) (paths map[string]string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "pybench-stripped-")
	if err != nil {
		return nil, nil, fmt.Errorf("pybench: create temp strip dir: %w", err)
	}
	cleanup = func() { os.RemoveAll(dir) }

	paths = make(map[string]string, len(cases))
	for _, c := range cases {
		source, err := os.ReadFile(c.Path)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("pybench: read %s: %w", c.Path, err)
		}
		strippedPath := filepath.Join(dir, filepath.Base(c.Path))
		if err := os.WriteFile(strippedPath, []byte(stripAnnotations(string(source))), 0o644); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("pybench: write %s: %w", strippedPath, err)
		}
		paths[c.Name] = strippedPath
	}
	return paths, cleanup, nil
}

// sourceFor resolves the path pybench should hand impl for case: the
// original source for every implementation except gpython, which runs the
// annotation-stripped copy (see Implementation.Strip).
func sourceFor(impl Implementation, c conformance.Case, stripped map[string]string) string {
	if impl.Strip {
		return stripped[c.Name]
	}
	return c.Path
}

// runProgram executes impl against sourcePath once and returns its captured
// stdout and wall-clock duration. Every implementation runs with its process
// working directory set to sourcePath's own directory and the source passed
// as a bare filename — not because it matters to most implementations, but
// because gpython's file resolver rejects some absolute paths outright
// (confirmed: identical absolute paths succeed or fail to resolve solely
// based on their prefix, not on any permission or existence problem) while
// always accepting a relative name in the file's own directory. Applying
// that shape uniformly keeps the invocation identical across
// implementations for a given case, which is what "same cwd" is for: a
// controlled, reproducible environment, not a single directory that would
// only work for the implementations that don't happen to have this quirk.
// Every implementation shares a fixed hash seed. A run exceeding timeout is
// killed and reported as an error (a slow, non-JIT implementation like
// gpython legitimately cannot finish every case in bounded time; that is
// reported as a correctness-gate failure, per gateCase's N/A handling for
// gpython, rather than left to hang the whole report).
func runProgram(ctx context.Context, impl Implementation, sourcePath string, timeout time.Duration) (stdout string, wall time.Duration, err error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := append(append([]string(nil), impl.Args...), filepath.Base(sourcePath))
	cmd := exec.CommandContext(runCtx, impl.Command, args...)
	cmd.Dir = filepath.Dir(sourcePath)
	cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	start := time.Now()
	err = cmd.Run()
	wall = time.Since(start)
	if runCtx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %s", timeout)
	}
	return out.String(), wall, err
}

// gateCase runs impl against c once and reports whether its output matches
// the CPython-derived golden. A mismatch or execution error is FAIL for
// every implementation except gpython, for which it is N/A: gpython's
// Python-3.4 ceiling (including simply being too slow to finish a 1-3s
// CPython workload within timeout) is an expected, documented limitation,
// not a minipy defect pybench should flag the same way.
func gateCase(ctx context.Context, impl Implementation, c conformance.Case, stripped map[string]string, timeout time.Duration) (CaseRow, bool) {
	sourcePath := sourceFor(impl, c, stripped)
	got, _, err := runProgram(ctx, impl, sourcePath, timeout)

	row := CaseRow{Case: c.Name, Impl: impl.Info.Name}
	switch {
	case err != nil:
		row.Status = failStatus(impl)
		row.Detail = err.Error()
		return row, false
	case got != c.Expected:
		row.Status = failStatus(impl)
		row.Detail = diffSummary(c.Expected, got)
		return row, false
	default:
		row.Status = StatusPass
		return row, true
	}
}

func failStatus(impl Implementation) Status {
	if impl.Strip {
		return StatusNA
	}
	return StatusFail
}

// diffSummary renders a short, single-line description of a correctness-gate
// mismatch for the report table: expected and got are already whole
// programs' worth of stdout, most often one line, so this reports them
// directly rather than computing a positional diff.
func diffSummary(expected, got string) string {
	return fmt.Sprintf("expected %q, got %q", oneLine(expected), oneLine(got))
}

// timeCase re-runs impl against c runs times and reduces the wall-clock
// durations to Stats. It is only called after gateCase has passed, so a
// timing-run error (distinct from the earlier correctness failure — e.g. a
// transient resource exhaustion) is reported by returning it rather than
// silently degrading the sample size.
func timeCase(ctx context.Context, impl Implementation, c conformance.Case, runs int, stripped map[string]string, timeout time.Duration) (Stats, error) {
	sourcePath := sourceFor(impl, c, stripped)
	durations := make([]time.Duration, 0, runs)
	for i := 0; i < runs; i++ {
		_, wall, err := runProgram(ctx, impl, sourcePath, timeout)
		if err != nil {
			return Stats{}, fmt.Errorf("pybench: %s timing run %d/%d on %s: %w", impl.Info.Name, i+1, runs, c.Name, err)
		}
		durations = append(durations, wall)
	}
	return computeStats(durations), nil
}

// gateAndTime runs the full correctness-gate-then-timing protocol for every
// (case, implementation) pair, in corpus order, and returns one CaseRow per
// pair. Progress is logged to progress as each pair completes, since a full
// run across every implementation can take many minutes.
func gateAndTime(ctx context.Context, impls []Implementation, cases []conformance.Case, runs int, stripped map[string]string, timeout time.Duration, progress io.Writer) []CaseRow {
	var rows []CaseRow
	for _, c := range cases {
		for _, impl := range impls {
			row, passed := gateCase(ctx, impl, c, stripped, timeout)
			if !passed {
				fmt.Fprintf(progress, "%-14s %-20s %s: %s\n", c.Name, impl.Info.Name, row.Status, row.Detail)
				rows = append(rows, row)
				continue
			}
			stats, err := timeCase(ctx, impl, c, runs, stripped, timeout)
			if err != nil {
				row.Status = failStatus(impl)
				row.Detail = err.Error()
				fmt.Fprintf(progress, "%-14s %-20s %s: %s\n", c.Name, impl.Info.Name, row.Status, row.Detail)
				rows = append(rows, row)
				continue
			}
			row.Stats = &stats
			fmt.Fprintf(progress, "%-14s %-20s %s: min=%s median=%s\n", c.Name, impl.Info.Name, row.Status, formatDuration(stats.Min), formatDuration(stats.Median))
			rows = append(rows, row)
		}
	}
	return rows
}

// measureStartups times startupSource under every implementation, the same
// way a benchmark case is timed, to establish each interpreter's fixed
// per-process baseline cost.
func measureStartups(ctx context.Context, impls []Implementation, runs int, timeout time.Duration, progress io.Writer) []StartupRow {
	dir, err := os.MkdirTemp("", "pybench-startup-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "startup.py")
	if err := os.WriteFile(path, []byte(startupSource), 0o644); err != nil {
		return nil
	}

	var rows []StartupRow
	for _, impl := range impls {
		durations := make([]time.Duration, 0, runs)
		ok := true
		for i := 0; i < runs; i++ {
			_, wall, err := runProgram(ctx, impl, path, timeout)
			if err != nil {
				ok = false
				break
			}
			durations = append(durations, wall)
		}
		if !ok {
			fmt.Fprintf(progress, "%-14s %-20s startup measurement failed\n", "(startup)", impl.Info.Name)
			continue
		}
		stats := computeStats(durations)
		fmt.Fprintf(progress, "%-14s %-20s startup min=%s median=%s\n", "(startup)", impl.Info.Name, formatDuration(stats.Min), formatDuration(stats.Median))
		rows = append(rows, StartupRow{Impl: impl.Info.Name, Stats: stats})
	}
	return rows
}
