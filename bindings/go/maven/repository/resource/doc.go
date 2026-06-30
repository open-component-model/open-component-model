// Package resource implements a repository.ResourceRepository for Maven
// artifacts. It downloads single release artifacts by GAV coordinates over
// HTTP(S) and deploys artifacts (with sha1/md5 checksums) to a Maven
// repository, resolving credentials via the "MavenRepository" consumer identity.
//
// Out of scope for now: SNAPSHOT resolution via maven-metadata.xml and
// whole-GAV enumeration.
package resource
