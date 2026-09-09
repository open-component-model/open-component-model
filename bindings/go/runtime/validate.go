package runtime

// Validatable is implemented by a type that can check its own consistency.
type Validatable interface {
	// Validate returns an error describing why the object is not valid, or nil if it is valid.
	Validate() error
}
