package scanner

import "regexp"

// makeImgRE extracts a registry-qualified image reference from a Makefile
// `IMG = …` / `IMG ?= …` / `IMG := …` assignment. The dotted-host requirement
// and the `[^\s#$]` value class drop unexpanded `$(VAR)` and bare names.
var makeImgRE = regexp.MustCompile(`(?m)^\s*IMG\s*[:?]?=\s*([a-z0-9.\-]+\.[a-z]{2,}/[^\s#$]+)`)

const makefileReadCap = 8 << 10 // 8 KiB

// dockerScanner recognises container images by their Dockerfile and enriches
// the candidate with an image reference discovered in a sibling Makefile.
type dockerScanner struct{}

func (dockerScanner) Name() string { return "docker" }

func (dockerScanner) Scan(fs *FileSet) []Candidate {
	var out []Candidate
	for _, f := range fs.Files {
		if baseName(f) != "Dockerfile" {
			continue
		}
		c := Candidate{
			Dir:       dirOf(f),
			Kind:      "ociImage",
			Priority:  PriorityOCIImage,
			Ecosystem: "docker",
			Runnable:  true,
			Note:      "detected a Dockerfile",
		}
		if b := fs.Read(sibling(f, "Makefile"), makefileReadCap); b != nil {
			for _, m := range makeImgRE.FindAllStringSubmatch(string(b), -1) {
				if len(m) > 1 {
					c.ImageHints = append(c.ImageHints, m[1])
				}
			}
		}
		c.ImageHints = dedup(c.ImageHints)
		out = append(out, c)
	}
	return out
}
