package blob

// DirOptions contains options for creating a blob from a directory.
type DirOptions struct {
	Path         string
	MediaType    string
	Compress     bool
	PreserveDir  bool
	Reproducible bool
	ExcludeFiles []string
	IncludeFiles []string
}
