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
		{Name: "ZeroDivisionError", Base: "Exception"},
		{Name: "ValueError", Base: "Exception"},
		{Name: "TypeError", Base: "Exception"},
		{Name: "IndexError", Base: "Exception"},
		{Name: "KeyError", Base: "Exception"},
		{Name: "RuntimeError", Base: "Exception"},
		{Name: "AssertionError", Base: "Exception"},
		{Name: "StopIteration", Base: "Exception"},
	}
}
