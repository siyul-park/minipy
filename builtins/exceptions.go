package builtins

// Exception describes a builtin exception class: its name and the name of its
// base class ("" for the root BaseException). The compiler seeds its class table
// from this list so exception identity lives in the builtins module rather than
// being hardcoded in the checker.
type Exception struct {
	Name string
	Base string
}

// Exceptions returns the builtin exception hierarchy in declaration order
// (base classes precede subclasses).
func Exceptions() []Exception {
	return []Exception{
		{Name: "BaseException", Base: ""},
		{Name: "Exception", Base: "BaseException"},
		{Name: "ArithmeticError", Base: "Exception"},
		{Name: "OverflowError", Base: "ArithmeticError"},
		{Name: "ZeroDivisionError", Base: "ArithmeticError"},
		{Name: "LookupError", Base: "Exception"},
		{Name: "IndexError", Base: "LookupError"},
		{Name: "KeyError", Base: "LookupError"},
		{Name: "AssertionError", Base: "Exception"},
		{Name: "TypeError", Base: "Exception"},
		{Name: "NameError", Base: "Exception"},
		{Name: "UnboundLocalError", Base: "NameError"},
		{Name: "ValueError", Base: "Exception"},
		{Name: "RuntimeError", Base: "Exception"},
		{Name: "StopIteration", Base: "Exception"},
	}
}
