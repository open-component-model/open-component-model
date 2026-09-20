package scanner

// rustScanner recognises Rust crates by their Cargo.toml. OCM has no first-class
// crate type, so the built artifact is modelled as a blob.
type rustScanner struct{}

func (rustScanner) Name() string { return "rust" }

func (rustScanner) Scan(fs *FileSet) []Candidate {
	var out []Candidate
	for _, f := range fs.Files {
		if baseName(f) != "Cargo.toml" {
			continue
		}
		out = append(out, Candidate{
			Dir:       dirOf(f),
			Kind:      "blob",
			Priority:  PriorityLangPkg,
			Ecosystem: "rust",
			Note:      "detected a Rust crate (Cargo.toml)",
		})
	}
	return out
}
