package scanner

import "regexp"

// chartNameRE extracts a Helm chart's name (the dash-tag namespace, e.g.
// grafana-10.4.0 -> "grafana").
var chartNameRE = regexp.MustCompile(`(?m)^\s*name:\s*["']?([A-Za-z0-9._-]+)`)

// chartAppVerRE extracts a chart's appVersion (an image-tag hint).
var chartAppVerRE = regexp.MustCompile(`(?m)^\s*appVersion:\s*["']?([^"'\s]+)`)

// imageRepoRE extracts a registry-qualified image repository from a values.yaml
// `repository:` field. It requires a dotted host (ghcr.io/…) so a bare Helm
// subchart `repository: <name>` is not mistaken for an image reference.
var imageRepoRE = regexp.MustCompile(`(?m)^\s*repository:\s*["']?([a-z0-9.\-]+\.[a-z]{2,}/[^\s"':]+)`)

const (
	chartReadCap  = 4 << 10 // 4 KiB
	valuesReadCap = 8 << 10 // 8 KiB
)

// helmScanner recognises Helm charts by their Chart.yaml.
type helmScanner struct{}

func (helmScanner) Name() string { return "helm" }

func (helmScanner) Scan(fs *FileSet) []Candidate {
	var out []Candidate
	for _, f := range fs.Files {
		if baseName(f) != "Chart.yaml" {
			continue
		}
		c := Candidate{
			Dir:       dirOf(f),
			Kind:      "helmChart",
			Priority:  PriorityHelmChart,
			Ecosystem: "helm",
			Note:      "detected a Chart.yaml",
		}
		if b := fs.Read(f, chartReadCap); b != nil {
			c.TagPrefix = firstSubmatch(chartNameRE.FindAllStringSubmatch(string(b), 1))
			if av := firstSubmatch(chartAppVerRE.FindAllStringSubmatch(string(b), 1)); av != "" && av != "0.0.0" {
				// appVersion is the app image tag hint, kept as a bare hint the
				// composer/assembler may combine with a discovered repository.
				c.Note = "detected a Chart.yaml (appVersion " + av + ")"
			}
		}
		if b := fs.Read(sibling(f, "values.yaml"), valuesReadCap); b != nil {
			for _, m := range imageRepoRE.FindAllStringSubmatch(string(b), -1) {
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
