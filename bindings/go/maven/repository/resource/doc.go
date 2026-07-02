// Package resource implements a repository.ResourceRepository for Maven
// artifacts. It downloads and uploads artifacts by GAV coordinates over
// HTTP(S) (with sha1/md5/sha256 checksums on upload), resolving credentials
// via the "MavenRepository" consumer identity.
//
// Resolution is coordinate-deterministic (no directory enumeration): releases
// resolve a single artifact; SNAPSHOT versions resolve via maven-metadata.xml,
// and a SNAPSHOT selector matching multiple files is returned as a tgz.
// Out of scope: file:// repositories, release multi-file enumeration, and
// SNAPSHOT deploy (metadata rewrite on upload).
//
// Digests computed over multi-file (tgz) selections cover the derived
// archive, not the individual upstream files, and are not stable across Go
// releases or SNAPSHOT redeployments.
package resource
