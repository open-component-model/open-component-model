package ocireference

func AsNamedTagged(ref Named) (nt NamedTagged, ok bool) {
	nt, ok = ref.(NamedTagged)
	return nt, ok
}

// FamiliarName returns the familiar name string
// for the given named, familiarizing if needed.
func FamiliarName(ref Named) string {
	if nn, ok := ref.(normalizedNamed); ok {
		return nn.Familiar().Name()
	}
	return ref.Name()
}

// FamiliarString returns the familiar string representation
// for the given reference, familiarizing if needed.
func FamiliarString(ref Reference) string {
	if nn, ok := ref.(normalizedNamed); ok {
		return nn.Familiar().String()
	}
	return ref.String()
}
