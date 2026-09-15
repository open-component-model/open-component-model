package maven

import "strings"

// SiblingSuffixes are the suffixes of the files a Maven repository publishes
// next to an artifact file: the detached PGP signature and the checksum
// files. A download fetches every sibling the repository has and stores it
// in the archive as-is; nothing is verified. Verification is the consumer's
// job (Maven itself checks checksums on resolve), and keeping the siblings
// untouched lets an upload mirror a repository entry exactly.
var SiblingSuffixes = []string{".asc", ".md5", ".sha1", ".sha256", ".sha512"}

// SplitSibling reports whether name is a sibling file, and if so the artifact
// file it belongs to and its suffix. A plain artifact file is returned as
// itself with an empty suffix.
func SplitSibling(name string) (file, suffix string) {
	for _, s := range SiblingSuffixes {
		if strings.HasSuffix(name, s) {
			return strings.TrimSuffix(name, s), s
		}
	}
	return name, ""
}
