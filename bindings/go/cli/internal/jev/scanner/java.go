package scanner

// javaScanner recognises Maven (pom.xml) and Gradle (build.gradle) projects.
// OCM has no first-class Maven/Gradle type, so the built artifact is modelled as
// a blob — but the note names the build system explicitly rather than falling
// through to a generic default.
type javaScanner struct{}

func (javaScanner) Name() string { return "java" }

func (javaScanner) Scan(fs *FileSet) []Candidate {
	var out []Candidate
	for _, f := range fs.Files {
		var note string
		switch baseName(f) {
		case "pom.xml":
			note = "detected a Maven project (pom.xml)"
		case "build.gradle", "build.gradle.kts":
			note = "detected a Gradle project (build.gradle)"
		default:
			continue
		}
		out = append(out, Candidate{
			Dir:       dirOf(f),
			Kind:      "blob",
			Priority:  PriorityLangPkg,
			Ecosystem: "java",
			Note:      note,
		})
	}
	return out
}
