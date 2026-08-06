// Command pybench runs minipy's benchmark corpus (conformance/testdata/benchmark)
// under every available Python-like implementation — CPython 3.13, CPython
// (whatever `python3` resolves to), pypy3, gpython, and minipy at -O0 and
// -O3 — and reports correctness and timing for each. See docs/benchmarks.md
// for methodology and results.
//
// Every implementation must first reproduce a case's CPython-derived golden
// exactly; only a match is timed; a mismatch is reported FAIL (or N/A for
// gpython, whose Python-3.4 ceiling is an expected, documented limitation)
// and never given a time.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/siyul-park/minipy/conformance"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "pybench:", err)
		os.Exit(1)
	}
}

// run parses flags, executes the full discover/build/gate/time/report flow,
// and writes the resulting Markdown report to out.
func run(args []string, out *os.File) error {
	corpus, runs, implFilter, format, timeout, err := parseFlags(args)
	if err != nil {
		return err
	}
	if format != "markdown" {
		return fmt.Errorf("unsupported -format %q: only \"markdown\" is implemented", format)
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}

	cases, err := conformance.Load(corpus)
	if err != nil {
		return fmt.Errorf("load corpus %s: %w", corpus, err)
	}
	if len(cases) == 0 {
		return fmt.Errorf("no benchmark cases found under %s", corpus)
	}

	ctx := context.Background()

	minipyBin, cleanupBin, err := buildMinipy(ctx, repoRoot)
	if err != nil {
		return err
	}
	defer cleanupBin()

	runnable, roster := discoverImplementations(ctx, minipyBin)
	runnable = filterImplementations(runnable, implFilter)
	if len(runnable) == 0 {
		return fmt.Errorf("no implementations left after -impl filter %q", implFilter)
	}

	stripped, cleanupStripped, err := prepareStrippedCopies(cases)
	if err != nil {
		return err
	}
	defer cleanupStripped()

	startups := measureStartups(ctx, runnable, runs, timeout, os.Stderr)
	rows := gateAndTime(ctx, runnable, cases, runs, stripped, timeout, os.Stderr)

	env := buildEnvironment(ctx, roster)
	fmt.Fprint(out, renderMarkdown(env, startups, rows))
	return nil
}

// parseFlags defines and parses pybench's CLI surface.
func parseFlags(args []string) (corpus string, runs int, implFilter, format string, timeout time.Duration, err error) {
	fs := flag.NewFlagSet("pybench", flag.ContinueOnError)
	fs.StringVar(&corpus, "corpus", "conformance/testdata/benchmark", "directory of `<name>.py`/`<name>.expected` benchmark cases")
	fs.IntVar(&runs, "runs", 5, "timing runs per (case, implementation) pair, after the correctness gate passes")
	fs.StringVar(&implFilter, "impl", "", "comma-separated substrings; only implementations whose name contains one are run (default: all discovered)")
	fs.StringVar(&format, "format", "markdown", "report format (only \"markdown\" is implemented)")
	fs.DurationVar(&timeout, "timeout", 40*time.Second, "per-process kill timeout; an implementation that cannot finish a case within it is reported FAIL/N-A, not left to hang the run")
	if err := fs.Parse(args); err != nil {
		return "", 0, "", "", 0, err
	}
	if runs < 1 {
		return "", 0, "", "", 0, fmt.Errorf("-runs must be >= 1, got %d", runs)
	}
	return corpus, runs, implFilter, format, timeout, nil
}

// filterImplementations keeps only implementations whose name contains one
// of filter's comma-separated, case-insensitive substrings. An empty filter
// keeps every implementation.
func filterImplementations(impls []Implementation, filter string) []Implementation {
	if filter == "" {
		return impls
	}
	var wants []string
	for _, w := range strings.Split(filter, ",") {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
			wants = append(wants, w)
		}
	}
	var kept []Implementation
	for _, impl := range impls {
		name := strings.ToLower(impl.Info.Name)
		for _, w := range wants {
			if strings.Contains(name, w) {
				kept = append(kept, impl)
				break
			}
		}
	}
	return kept
}

// buildEnvironment gathers the host facts that belong in the report header:
// the date this run started, kernel version, CPU count, and RAM size.
func buildEnvironment(ctx context.Context, roster []ImplInfo) Environment {
	return Environment{
		Date:   time.Now().Format("2006-01-02 15:04 MST"),
		Kernel: hostKernel(ctx),
		CPUs:   runtime.NumCPU(),
		RAMGiB: hostRAMGiB(),
		Impls:  roster,
	}
}

// hostKernel reports `uname -sr` output, or "unknown" if it cannot be run
// (e.g. on a non-Unix host).
func hostKernel(ctx context.Context) string {
	if v := detectVersion(ctx, "uname", "-sr"); v != "" {
		return v
	}
	return "unknown"
}

// hostRAMGiB reads total installed RAM from /proc/meminfo (Linux). It
// returns 0 when that file is unavailable, which the report renders as
// "0.0 GiB" rather than failing the whole run over a cosmetic header field.
func hostRAMGiB() float64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kib, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0
		}
		return kib / (1024 * 1024)
	}
	return 0
}
