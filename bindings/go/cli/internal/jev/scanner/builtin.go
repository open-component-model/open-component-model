package scanner

// DefaultRegistry returns the built-in scanner registry. Registration order is
// the tie-break for equal-priority candidates in the same directory; kinds have
// distinct priorities, so order does not affect kind ranking. Add an ecosystem
// by registering its scanner here.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(helmScanner{})
	r.Register(dockerScanner{})
	r.Register(goScanner{})
	r.Register(nodeScanner{})
	r.Register(pythonScanner{})
	r.Register(rustScanner{})
	r.Register(javaScanner{})
	return r
}
