package scanner

// nodeScanner recognises Node/npm packages by their package.json.
type nodeScanner struct{}

func (nodeScanner) Name() string { return "node" }

func (nodeScanner) Scan(fs *FileSet) []Candidate {
	var out []Candidate
	for _, f := range fs.Files {
		if baseName(f) != "package.json" {
			continue
		}
		out = append(out, Candidate{
			Dir:       dirOf(f),
			Kind:      "npmPackage",
			Priority:  PriorityLangPkg,
			Ecosystem: "node",
			Note:      "detected a package.json",
		})
	}
	return out
}
