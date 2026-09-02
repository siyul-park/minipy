package codegen

import (
	"fmt"
	"strings"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	vmtypes "github.com/siyul-park/minivm/types"
)

// Format renders a compiled program as the corpus golden text: a metrics
// summary, then the entry code, the declared slot tables, and the pools.
//
// minivm's own (*program.Program).String() is the model for the section layout
// and the instruction lines, which come from instr.Format either way. This
// renderer exists because a minipy program's constant pool is full of host
// functions, whose String() spans two lines and repeats its own signature;
// collapsing each constant onto one line is what makes a golden diff readable.
// The listing is a record of the compiler's output, not a wire format: it is
// never parsed back.
func Format(prog *program.Program) string {
	var out strings.Builder
	writeMetrics(&out, Measure(prog))

	out.WriteString("\n.code\n")
	out.WriteString(instr.Format(prog.Code))

	writeTypes(&out, ".locals", prog.Locals)
	writeTypes(&out, ".globals", prog.Globals)
	writeConstants(&out, prog.Constants)
	writeTypes(&out, ".types", prog.Types)
	writeHandlers(&out, prog.Handlers)
	return out.String()
}

// Metrics is the size of a compiled program along the axes a codegen change
// moves. It heads every golden so a diff says what grew before it says where.
type Metrics struct {
	Instructions  int // decoded instructions in the entry code and every function
	Functions     int // minipy function constants
	HostFunctions int // native host-function constants
	Constants     int // constant pool entries
	Types         int // runtime type pool entries
	Globals       int // declared module global slots
	Locals        int // declared entry-frame local slots
	Handlers      int // top-level exception table entries
}

// Measure counts a program along every Metrics axis.
func Measure(prog *program.Program) Metrics {
	metrics := Metrics{
		Instructions: len(instr.Unmarshal(prog.Code)),
		Constants:    len(prog.Constants),
		Types:        len(prog.Types),
		Globals:      len(prog.Globals),
		Locals:       len(prog.Locals),
		Handlers:     len(prog.Handlers),
	}
	for _, constant := range prog.Constants {
		switch constant := constant.(type) {
		case *vmtypes.Function:
			metrics.Functions++
			metrics.Instructions += len(instr.Unmarshal(constant.Code))
		case *interp.HostFunction:
			metrics.HostFunctions++
		}
	}
	return metrics
}

func writeMetrics(out *strings.Builder, metrics Metrics) {
	for _, row := range []struct {
		name  string
		count int
	}{
		{"instructions", metrics.Instructions},
		{"functions", metrics.Functions},
		{"host functions", metrics.HostFunctions},
		{"constants", metrics.Constants},
		{"types", metrics.Types},
		{"globals", metrics.Globals},
		{"locals", metrics.Locals},
		{"handlers", metrics.Handlers},
	} {
		fmt.Fprintf(out, "# %-14s %d\n", row.name, row.count)
	}
}

func writeTypes(out *strings.Builder, section string, types []vmtypes.Type) {
	if len(types) == 0 {
		return
	}
	fmt.Fprintf(out, "\n%s\n", section)
	for index, typ := range types {
		fmt.Fprintf(out, "%04d:\t%s\n", index, typ)
	}
}

// writeConstants lists the constant pool, disassembling a minipy function's
// body under its signature the way minivm's own listing nests it. A host
// function is one line: it has no bytecode to show, only the signature the
// checker and emitter agreed on.
func writeConstants(out *strings.Builder, constants []vmtypes.Value) {
	if len(constants) == 0 {
		return
	}
	out.WriteString("\n.constants\n")
	for index, constant := range constants {
		switch constant := constant.(type) {
		case *vmtypes.Function:
			fmt.Fprintf(out, "%04d:\t%s\n", index, constant.Typ)
			for _, line := range strings.Split(strings.TrimRight(instr.Format(constant.Code), "\n"), "\n") {
				fmt.Fprintf(out, "\t%s\n", line)
			}
		case *interp.HostFunction:
			fmt.Fprintf(out, "%04d:\t<host> %s\n", index, constant.Typ)
		default:
			fmt.Fprintf(out, "%04d:\t%s %s\n", index, constant.Type(), oneLine(constant.String()))
		}
	}
}

func writeHandlers(out *strings.Builder, handlers []instr.Handler) {
	if len(handlers) == 0 {
		return
	}
	out.WriteString("\n.handlers\n")
	for index, handler := range handlers {
		fmt.Fprintf(out, "%04d:\tstart=%d end=%d catch=%d depth=%d\n",
			index, handler.Start, handler.End, handler.Catch, handler.Depth)
	}
}

// oneLine keeps a constant's rendering on the single line its index labels, so
// a multi-line value cannot be mistaken for further pool entries.
func oneLine(text string) string {
	return strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", " ")
}
