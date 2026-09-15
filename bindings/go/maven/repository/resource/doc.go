// Package resource implements a repository.ResourceRepository for Maven
// artifacts addressed by the maven/v2alpha1 access type.
//
// # Scope
//
// The access spec names the artifact by GAV coordinates and lists the files it
// wants as (extension, classifier) pairs. Every entry maps to exactly one file
// in the repository, so resolution never lists directories. Releases resolve
// from the coordinates alone. SNAPSHOT versions resolve their timestamped
// file names from the version-level maven-metadata.xml. LATEST and RELEASE
// resolve from the artifact-level maven-metadata.xml.
//
// Every file is also fetched together with the sibling files the repository
// publishes next to it: the ".asc" signature and the checksum files (.md5,
// .sha1, .sha256, .sha512). Siblings are never listed in the spec and never
// verified; they are stored exactly as served, so an archive mirrors the
// repository entry and a consumer (Maven itself, or a signing tool) can check
// them. A 404 means the repository has no such sibling; any other failure is
// an error.
//
// Download always returns one application/x-tgz archive, even for a single
// file without siblings, with entries in spec order and each file followed by
// its siblings. One shape for every download means consumers never branch on
// the entry count. The archive is built with stored gzip blocks and fixed tar
// headers, so its bytes depend only on the files fetched, not on the Go
// release that built it. They do depend on which siblings the repository
// serves, and a SNAPSHOT or a LATEST/RELEASE lookup resolves to different
// files over time; see Maven.IsPinnedVersion.
//
// Upload is the inverse of download. It takes the same application/x-tgz,
// checks that it holds exactly the files the spec lists plus their optional
// siblings, and writes every entry unchanged at the release path. No
// checksums are computed. Entries are written one by one and a failing PUT
// leaves the earlier ones deployed. This is what a by-value transfer needs to
// push a Maven resource into another repository. Out of scope: file://
// repositories, LATEST and RELEASE (they name no version directory), and
// SNAPSHOT deploy (it needs the version-level maven-metadata.xml rewritten).
//
// # Credentials
//
// Credentials are resolved through the "MavenRepository" consumer identity
// built from repoUrl and decoded once at the edge into MavenCredentials/v1
// (spec/credentials/v1); a DirectCredentials property bag is accepted too,
// including old OCM's "accessToken" key. An identityToken yields Bearer auth;
// username and password yield Basic auth. Nil credentials, and a property bag
// without any Maven key, mean an anonymous request. Credentials that carry a
// password but neither a token nor a username are an error, not a silent
// anonymous request.
//
// # Usage
//
//	repo := resource.NewResourceRepository()
//	res := &descriptor.Resource{Access: &v2alpha1.Maven{
//		Type:       runtime.NewVersionedType(v2alpha1.Type, v2alpha1.Version),
//		RepoURL:    "https://repo1.maven.org/maven2",
//		GroupID:    "org.springframework.kafka",
//		ArtifactID: "spring-kafka",
//		Version:    "4.0.1",
//		Artifacts: []v2alpha1.Artifact{
//			{Extension: "jar"},
//			{Extension: "jar", Classifier: "sources"},
//		},
//	}}
//	b, err := repo.DownloadResource(ctx, res, nil) // application/x-tgz: both jars, each followed by its .asc and checksum files
//
// # Registration
//
// The CLI registers this repository together with the Maven digest processor
// and the MavenCredentials/v1 credential scheme in its builtin plugin set.
package resource
