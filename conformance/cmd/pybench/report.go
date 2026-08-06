package main

import (
	"fmt"
	"strings"
	"time"
)

// Environment captures the host and toolchain facts printed at the top of a
// report, so a reader can tell what a set of numbers was measured on without
// re-running anything.
type Environment struct {
	Date   string
	Kernel string
	CPUs   int
	RAMGiB float64
	Impls  []ImplInfo
}

// ImplInfo names one implementation considered for the run: either a
// resolved binary with its --version output, or one reported absent because
// it was not found on PATH.
type ImplInfo struct {
	Name    string
	Version string
	Absent  bool
}

// StartupRow is one implementation's timing on the trivial baseline program,
// used both as its own report section and to compute the
// wall-clock-minus-startup column on every benchmark row.
type StartupRow struct {
	Impl  string
	Stats Stats
}

// CaseRow is one (benchmark, implementation) correctness-and-timing result.
// Stats is nil unless Status is StatusPass, since the correctness gate never
// records a time for output that did not match.
type CaseRow struct {
	Case   string
	Impl   string
	Status Status
	Detail string
	Stats  *Stats
}

// Status is a CaseRow's correctness-gate outcome.
type Status string

const (
	StatusPass Status = "PASS"
	StatusFail Status = "FAIL"
	StatusNA   Status = "N/A"
)

// renderMarkdown formats env, the startup baseline, and every benchmark's
// per-implementation rows as a Markdown report: a header, a startup table,
// then one correctness-and-timing table per benchmark in corpus order.
func renderMarkdown(env Environment, startups []StartupRow, rows []CaseRow) string {
	var b strings.Builder
	writeHeader(&b, env)
	writeStartupTable(&b, startups)
	writeCaseTables(&b, startups, rows)
	return b.String()
}

func writeHeader(b *strings.Builder, env Environment) {
	fmt.Fprintf(b, "# minipy cross-implementation benchmark report\n\n")
	fmt.Fprintf(b, "- Date: %s\n", env.Date)
	fmt.Fprintf(b, "- Kernel: %s\n", env.Kernel)
	fmt.Fprintf(b, "- CPUs: %d\n", env.CPUs)
	fmt.Fprintf(b, "- RAM: %.1f GiB\n", env.RAMGiB)
	fmt.Fprintf(b, "- Implementations:\n")
	for _, impl := range env.Impls {
		if impl.Absent {
			fmt.Fprintf(b, "  - %s: not found on PATH\n", impl.Name)
			continue
		}
		fmt.Fprintf(b, "  - %s: %s\n", impl.Name, oneLine(impl.Version))
	}
	b.WriteString("\n")
}

func writeStartupTable(b *strings.Builder, startups []StartupRow) {
	if len(startups) == 0 {
		return
	}
	b.WriteString("## Startup baseline (empty program)\n\n")
	b.WriteString("| Implementation | Min | Median |\n")
	b.WriteString("|---|---:|---:|\n")
	for _, s := range startups {
		fmt.Fprintf(b, "| %s | %s | %s |\n", s.Impl, formatDuration(s.Stats.Min), formatDuration(s.Stats.Median))
	}
	b.WriteString("\n")
}

// writeCaseTables groups rows by benchmark (preserving first-seen order,
// which callers populate in corpus order) and renders one table per
// benchmark, each row showing the correctness gate result and, only for a
// pass, wall-clock and wall-clock-minus-startup timing.
func writeCaseTables(b *strings.Builder, startups []StartupRow, rows []CaseRow) {
	startupByImpl := make(map[string]Stats, len(startups))
	for _, s := range startups {
		startupByImpl[s.Impl] = s.Stats
	}

	for _, caseName := range caseOrder(rows) {
		fmt.Fprintf(b, "## %s\n\n", caseName)
		b.WriteString("| Implementation | Status | Min | Median | Min - startup | Median - startup |\n")
		b.WriteString("|---|---|---:|---:|---:|---:|\n")
		for _, r := range rows {
			if r.Case != caseName {
				continue
			}
			writeCaseRow(b, r, startupByImpl[r.Impl])
		}
		b.WriteString("\n")
	}
}

func writeCaseRow(b *strings.Builder, r CaseRow, startup Stats) {
	if r.Stats == nil {
		fmt.Fprintf(b, "| %s | %s%s | - | - | - | - |\n", r.Impl, r.Status, detailSuffix(r.Detail))
		return
	}
	fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s |\n",
		r.Impl, r.Status,
		formatDuration(r.Stats.Min), formatDuration(r.Stats.Median),
		formatDuration(minusStartup(r.Stats.Min, startup.Min)), formatDuration(minusStartup(r.Stats.Median, startup.Median)),
	)
}

func detailSuffix(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + oneLine(detail) + ")"
}

// caseOrder returns each distinct CaseRow.Case in first-seen order.
func caseOrder(rows []CaseRow) []string {
	var order []string
	seen := make(map[string]bool)
	for _, r := range rows {
		if !seen[r.Case] {
			seen[r.Case] = true
			order = append(order, r.Case)
		}
	}
	return order
}

// formatDuration renders d in milliseconds with one decimal place, the
// natural unit for both interpreter startup and the corpus's 1-3s programs.
func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000.0)
}

// oneLine collapses multi-line --version output (pypy3 and gpython both emit
// several lines) into one, since it is embedded in a Markdown list item or
// table cell.
func oneLine(s string) string {
	fields := strings.Fields(strings.ReplaceAll(s, "\n", " "))
	return strings.Join(fields, " ")
}
