package scanner

// pythonScanner recognises Python distributions by pyproject.toml or setup.py.
type pythonScanner struct{}

func (pythonScanner) Name() string { return "python" }

func (pythonScanner) Scan(fs *FileSet) []Candidate {
	var out []Candidate
	for _, f := range fs.Files {
		if n := baseName(f); n != "pyproject.toml" && n != "setup.py" {
			continue
		}
		out = append(out, Candidate{
			Dir:       dirOf(f),
			Kind:      "pythonPackage",
			Priority:  PriorityLangPkg,
			Ecosystem: "python",
			Note:      "detected a Python distribution (pyproject.toml/setup.py)",
		})
	}
	return out
}
