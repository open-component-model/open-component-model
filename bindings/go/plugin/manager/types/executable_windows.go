package types

// ExecutableSuffix is the file name extension executables carry on this OS.
const ExecutableSuffix = ".exe"

// ExecutableName returns the file name of an executable with the given base name on this OS.
func ExecutableName(name string) string {
	return name + ExecutableSuffix
}
