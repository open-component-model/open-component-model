// Package integration contains integration tests for the Maven binding that
// exercise the resource repository against a REAL Maven CLI running in a
// container (testcontainers). It validates true interoperability in both
// directions:
//
//   - download: real `mvn deploy:deploy-file` publishes an artifact into a real
//     Maven repository server (Sonatype Nexus Repository), and our
//     DownloadResource fetches it back.
//   - upload: our UploadResource deploys a download archive (every file with
//     the sibling checksum files it carries, unchanged) into the same Nexus
//     server, and real `mvn dependency:get -C` (strict checksums) consumes and
//     validates it.
//
// The repository server is a real Nexus Repository 3 instance running in its
// own container. The Go code under test reaches it over the mapped host port
// (loopback), while the Maven client container reaches it via testcontainers'
// host-access feature (host.testcontainers.internal).
package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/repository/resource"
	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mavenImage is the real Maven CLI used as the interop oracle. The tag must be
// multi-arch: integration CI runs on arm64, and the "-alpine" variants are
// amd64-only, so under emulation the JVM cannot complete a TLS handshake to
// fetch Maven's own plugins.
const mavenImage = "maven:3.9.16-eclipse-temurin-11"

// nexusImage is a real Maven repository server used as the repository under
// test. Sonatype's Community Edition image is free and multi-arch. The tag is
// pinned because "latest" moves.
const nexusImage = "sonatype/nexus3:3.96.1"

// username and password are the Nexus admin credentials. Setting
// NEXUS_SECURITY_RANDOMPASSWORD=false makes Nexus use this fixed password
// instead of writing a random one into the container.
const (
	username = "admin"
	password = "admin123"
	// repoName is the hosted repository the tests create for deploys and
	// downloads. It is created with MIXED version policy so both release and
	// SNAPSHOT deploys land in the same repository.
	repoName = "ocm-releases"
	// transferRepoName is a second hosted repository, the target of the
	// transfer test.
	transferRepoName = "ocm-transfer"
)

// settingsXML disables Maven's default HTTP-repository blocker (empty mirrors)
// so the test can use a plain-HTTP repository and provides credentials for the
// "it" server id used by deploy/get. Passed as both user (-s) and global (-gs)
// settings to fully replace the bundled blocker mirror.
var settingsXML = fmt.Sprintf(`<settings xmlns="http://maven.apache.org/SETTINGS/1.0.0">
  <servers>
    <server>
      <id>it</id>
      <username>%s</username>
      <password>%s</password>
    </server>
  </servers>
  <mirrors></mirrors>
</settings>
`, username, password)

// repoServer is a real Nexus Repository 3 server running in a container.
type repoServer struct {
	c      testcontainers.Container
	port   int
	client *http.Client
}

func newRepoServer(t *testing.T, ctx context.Context) *repoServer {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        nexusImage,
		ExposedPorts: []string{"8081/tcp"},
		Env: map[string]string{
			"NEXUS_SECURITY_RANDOMPASSWORD": "false",
		},
		// Nexus takes one to two minutes to boot. The status endpoint answers
		// 200 without authentication once the server is serving requests.
		WaitingFor: wait.ForHTTP("/service/rest/v1/status").
			WithPort("8081/tcp").
			WithStartupTimeout(5 * time.Minute),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testcontainers.TerminateContainer(c)) })

	mapped, err := c.MappedPort(ctx, "8081")
	require.NoError(t, err)

	rs := &repoServer{c: c, port: int(mapped.Num()), client: &http.Client{}}
	rs.acceptEULA(t, ctx)
	rs.createHostedRepository(t, ctx, repoName)
	rs.createHostedRepository(t, ctx, transferRepoName)
	return rs
}

// acceptEULA accepts the Community Edition license through the REST API.
// Until it is accepted Nexus answers every content request, even from admin,
// with 403 and a pointer to the onboarding wizard. The API returns the EULA
// object; posting it back with "accepted" set to true is the acceptance.
func (rs *repoServer) acceptEULA(t *testing.T, ctx context.Context) {
	t.Helper()
	eulaURL := fmt.Sprintf("http://127.0.0.1:%d/service/rest/v1/system/eula", rs.port)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eulaURL, nil)
	require.NoError(t, err)
	req.SetBasicAuth(username, password)
	resp, err := rs.client.Do(req)
	require.NoError(t, err)
	var eula map[string]any
	err = json.NewDecoder(resp.Body).Decode(&eula)
	_ = resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	eula["accepted"] = true
	body, err := json.Marshal(eula)
	require.NoError(t, err)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, eulaURL, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(username, password)
	resp, err = rs.client.Do(req)
	require.NoError(t, err)
	out, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equalf(t, http.StatusNoContent, resp.StatusCode, "accepting EULA: status %d: %s", resp.StatusCode, out)
}

// createHostedRepository creates a hosted Maven repository through the Nexus
// REST API. Strict content-type validation is off because the tests deploy
// synthetic payloads (plain text named .jar) that Nexus would otherwise reject
// as not matching their declared type. The default write policy, allow once,
// keeps Nexus's real rule that a release file is never overwritten.
func (rs *repoServer) createHostedRepository(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	body := fmt.Sprintf(`{
  "name": %q,
  "online": true,
  "storage": {"blobStoreName": "default", "strictContentTypeValidation": false, "writePolicy": "ALLOW_ONCE"},
  "maven": {"versionPolicy": "MIXED", "layoutPolicy": "STRICT", "contentDisposition": "INLINE"}
}`, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/service/rest/v1/repositories/maven/hosted", rs.port), strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(username, password)
	resp, err := rs.client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	require.Equalf(t, http.StatusCreated, resp.StatusCode, "creating hosted repository %q: status %d: %s", name, resp.StatusCode, out)
}

// hostURL is the base repository URL as seen by the Go code under test (host loopback).
func (rs *repoServer) hostURL() string { return rs.hostURLFor(repoName) }

// hostURLFor is hostURL for another hosted repository of the same server.
func (rs *repoServer) hostURLFor(repo string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/repository/%s", rs.port, repo)
}

// containerURL is the base repository URL as seen from inside the Maven container.
func (rs *repoServer) containerURL() string { return rs.containerURLFor(repoName) }

// containerURLFor is containerURL for another hosted repository of the same server.
func (rs *repoServer) containerURLFor(repo string) string {
	return fmt.Sprintf("http://host.testcontainers.internal:%d/repository/%s", rs.port, repo)
}

// writeFile uploads a file into the repository via an authenticated HTTP PUT.
// Used to seed a .pom (and its checksums) so that `mvn dependency:get` can
// resolve an artifact our code uploaded.
func (rs *repoServer) writeFile(t *testing.T, ctx context.Context, relPath string, data []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rs.hostURL()+"/"+relPath, bytes.NewReader(data))
	require.NoError(t, err)
	req.SetBasicAuth(username, password)
	resp, err := rs.client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Truef(t, resp.StatusCode >= 200 && resp.StatusCode < 300, "unexpected status %d uploading %q", resp.StatusCode, relPath)
}

// readFile downloads a file from the repository via an authenticated HTTP GET.
func (rs *repoServer) readFile(t *testing.T, ctx context.Context, relPath string) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rs.hostURL()+"/"+relPath, nil)
	require.NoError(t, err)
	req.SetBasicAuth(username, password)
	resp, err := rs.client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equalf(t, http.StatusOK, resp.StatusCode, "unexpected status %d reading %q", resp.StatusCode, relPath)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return data
}

// mavenContainer is a long-lived Maven CLI container we exec commands into.
type mavenContainer struct {
	c testcontainers.Container
}

func startMavenContainer(t *testing.T, ctx context.Context, hostPort int) *mavenContainer {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image: mavenImage,
		// Keep the container alive so we can exec multiple mvn invocations.
		Entrypoint: []string{"sleep"},
		Cmd:        []string{"infinity"},
		// Allow the container to reach the host's repo server.
		HostAccessPorts: []int{hostPort},
		WaitingFor:      wait.ForExec([]string{"sh", "-c", "command -v mvn"}),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testcontainers.TerminateContainer(c)) })

	mc := &mavenContainer{c: c}
	// Seed the settings.xml used to bypass the HTTP blocker and provide credentials.
	require.NoError(t, c.CopyToContainer(ctx, []byte(settingsXML), "/work/settings.xml", 0o644))
	return mc
}

// mvn runs an mvn invocation in the container and returns combined output. It
// always passes the permissive settings as both user and global settings.
func (mc *mavenContainer) mvn(t *testing.T, ctx context.Context, args ...string) (int, string) {
	t.Helper()
	full := append([]string{"mvn", "-B", "-s", "/work/settings.xml", "-gs", "/work/settings.xml"}, args...)
	code, reader, err := mc.c.Exec(ctx, full)
	require.NoError(t, err)
	out, _ := io.ReadAll(reader)
	return code, string(out)
}

// deployCredentials returns the credentials used by our resource repository to
// authenticate write operations against Nexus.
func deployCredentials() runtime.Typed {
	return &credv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credv1.CredentialsType, credv1.Version),
		Properties: map[string]string{"username": username, "password": password},
	}
}

func Test_Integration_MavenResourceRepository(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping maven integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	rs := newRepoServer(t, ctx)
	mc := startMavenContainer(t, ctx, rs.port)
	repo := resource.NewResourceRepository()

	// Direction 1: real Maven deploys -> our DownloadResource reads it back.
	t.Run("download artifact deployed by real maven", func(t *testing.T) {
		const (
			group    = "com.example"
			artifact = "downloaded"
			version  = "1.0.0"
		)
		artifactBytes := []byte("hello-from-real-maven")
		require.NoError(t, mc.c.CopyToContainer(ctx, artifactBytes, "/work/downloaded.jar", 0o644))

		code, out := mc.mvn(t, ctx, "deploy:deploy-file",
			"-Dfile=/work/downloaded.jar",
			"-DgroupId="+group,
			"-DartifactId="+artifact,
			"-Dversion="+version,
			"-Dpackaging=jar",
			"-DrepositoryId=it",
			"-Durl="+rs.containerURL(),
		)
		require.Equalf(t, 0, code, "mvn deploy:deploy-file failed:\n%s", out)

		res := mavenResource(rs.hostURL(), group, artifact, version)
		b, err := repo.DownloadResource(ctx, res, deployCredentials())
		require.NoError(t, err)
		data := entryOf(t, b, artifact+"-"+version+".jar")
		assert.Equal(t, artifactBytes, data, "downloaded bytes must match what maven deployed")
	})

	// Direction 2: our UploadResource publishes -> real Maven consumes & validates checksums.
	t.Run("upload artifact consumed and checksum-validated by real maven", func(t *testing.T) {
		const (
			group    = "com.example"
			artifact = "uploaded"
			version  = "2.0.0"
		)
		artifactBytes := []byte("uploaded-by-ocm")

		res := mavenResource(rs.hostURL(), group, artifact, version)
		_, err := repo.UploadResource(ctx, res, archiveOf(t, artifact+"-"+version+".jar", artifactBytes), deployCredentials())
		require.NoError(t, err)

		// Seed a minimal pom (+ checksums) so dependency:get can resolve the GAV.
		// The spec lists only the jar, so the archive holds the jar and its
		// checksum siblings.
		base := fmt.Sprintf("%s/%s/%s/%s-%s",
			strings.ReplaceAll(group, ".", "/"), artifact, version, artifact, version)
		pom := []byte(fmt.Sprintf(`<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>%s</groupId>
  <artifactId>%s</artifactId>
  <version>%s</version>
  <packaging>jar</packaging>
</project>
`, group, artifact, version))
		rs.writeFile(t, ctx, base+".pom", pom)
		rs.writeFile(t, ctx, base+".pom.sha1", []byte(fmt.Sprintf("%x", sha1.Sum(pom))))
		rs.writeFile(t, ctx, base+".pom.md5", []byte(fmt.Sprintf("%x", md5.Sum(pom))))

		// Sanity: our upload deployed the artifact and the checksum siblings the
		// archive carried, unchanged.
		gotJar := rs.readFile(t, ctx, base+".jar")
		assert.Equal(t, artifactBytes, gotJar)
		assert.Equal(t, fmt.Sprintf("%x", sha1.Sum(artifactBytes)), string(rs.readFile(t, ctx, base+".jar.sha1")))
		assert.Equal(t, fmt.Sprintf("%x", md5.Sum(artifactBytes)), string(rs.readFile(t, ctx, base+".jar.md5")))

		// -C: strict checksum policy -> fail the build on any checksum mismatch.
		code, out := mc.mvn(t, ctx, "-C", "dependency:get",
			"-DremoteRepositories=it::::"+rs.containerURL(),
			"-Dartifact="+fmt.Sprintf("%s:%s:%s", group, artifact, version),
			"-Dtransitive=false",
		)
		require.Equalf(t, 0, code, "mvn dependency:get (strict checksums) failed:\n%s", out)

		// The hosted repository is created with the ALLOW_ONCE write policy, so
		// deploying the same release again is refused by the server and must
		// surface as an error rather than a silent no-op.
		_, err = repo.UploadResource(ctx, res, archiveOf(t, artifact+"-"+version+".jar", artifactBytes), deployCredentials())
		require.ErrorContains(t, err, "unexpected status")
	})

	// Direction 3: classifier + non-jar extension exercised across the Maven
	// boundary in both directions. The default-jar subtests above never resolve
	// a classifier or a non-"jar" extension, so this pins down that our
	// coordinate->URL layout (group/artifact/version/artifact-version-classifier.ext)
	// matches real Maven's on-disk layout for a classified, non-jar artifact.
	t.Run("round-trip artifact with classifier and non-jar extension", func(t *testing.T) {
		const (
			group      = "com.example"
			artifact   = "classified"
			version    = "3.0.0"
			classifier = "dist"
			extension  = "zip"
		)
		artifactBytes := []byte("classified-non-jar-payload")

		// Leg 1 (maven -> us): real Maven deploys com.example:classified:3.0.0:zip:dist,
		// and our DownloadResource resolves that exact classifier+extension path.
		require.NoError(t, mc.c.CopyToContainer(ctx, artifactBytes, "/work/classified.zip", 0o644))
		code, out := mc.mvn(t, ctx, "deploy:deploy-file",
			"-Dfile=/work/classified.zip",
			"-DgroupId="+group,
			"-DartifactId="+artifact,
			"-Dversion="+version,
			"-Dpackaging="+extension,
			"-Dclassifier="+classifier,
			"-DrepositoryId=it",
			"-Durl="+rs.containerURL(),
		)
		require.Equalf(t, 0, code, "mvn deploy:deploy-file (classifier) failed:\n%s", out)

		res := mavenResource(rs.hostURL(), group, artifact, version,
			v2alpha1.Artifact{Extension: extension, Classifier: classifier})
		b, err := repo.DownloadResource(ctx, res, deployCredentials())
		require.NoError(t, err)
		assert.Equal(t, artifactBytes, entryOf(t, b, artifact+"-"+version+"-"+classifier+"."+extension), "download must resolve the classifier+extension path maven wrote")

		// Leg 2 (us -> us): our UploadResource publishes the same coordinates to a
		// fresh version and our DownloadResource reads it back, confirming upload
		// and download agree on the classifier+extension path against a real repo.
		const uploadVersion = "3.0.1"
		upload := mavenResource(rs.hostURL(), group, artifact, uploadVersion,
			v2alpha1.Artifact{Extension: extension, Classifier: classifier})
		_, err = repo.UploadResource(ctx, upload,
			archiveOf(t, artifact+"-"+uploadVersion+"-"+classifier+"."+extension, artifactBytes), deployCredentials())
		require.NoError(t, err)

		// The artifact and its checksum siblings must land at the classified path.
		base := fmt.Sprintf("%s/%s/%s/%s-%s-%s",
			strings.ReplaceAll(group, ".", "/"), artifact, uploadVersion, artifact, uploadVersion, classifier)
		assert.Equal(t, artifactBytes, rs.readFile(t, ctx, base+"."+extension))
		assert.Equal(t, fmt.Sprintf("%x", sha1.Sum(artifactBytes)), string(rs.readFile(t, ctx, base+"."+extension+".sha1")))
		assert.Equal(t, fmt.Sprintf("%x", md5.Sum(artifactBytes)), string(rs.readFile(t, ctx, base+"."+extension+".md5")))

		down, err := repo.DownloadResource(ctx, upload, deployCredentials())
		require.NoError(t, err)
		assert.Equal(t, artifactBytes, entryOf(t, down, artifact+"-"+uploadVersion+"-"+classifier+"."+extension), "uploaded classifier+extension artifact must round-trip")
	})

	// Direction 6: the transfer shape. The archive DownloadResource produced
	// for a jar that real Maven deployed (with its .asc) is deployed unchanged
	// into another hosted repository of the same server, then real Maven
	// resolves it from there with strict checksums.
	t.Run("download archive is deployed unchanged into another repository", func(t *testing.T) {
		const (
			group    = "com.example"
			artifact = "transferred"
			version  = "6.0.0"
		)
		artifactBytes := []byte("transfer-me")
		signature := []byte("-----BEGIN PGP SIGNATURE-----\ntransfer\n-----END PGP SIGNATURE-----\n")
		require.NoError(t, mc.c.CopyToContainer(ctx, artifactBytes, "/work/transferred.jar", 0o644))
		code, out := mc.mvn(t, ctx, "deploy:deploy-file",
			"-Dfile=/work/transferred.jar",
			"-DgroupId="+group,
			"-DartifactId="+artifact,
			"-Dversion="+version,
			"-Dpackaging=jar",
			"-DrepositoryId=it",
			"-Durl="+rs.containerURL(),
		)
		require.Equalf(t, 0, code, "mvn deploy:deploy-file failed:\n%s", out)
		base := fmt.Sprintf("%s/%s/%s/%s-%s",
			strings.ReplaceAll(group, ".", "/"), artifact, version, artifact, version)
		rs.writeFile(t, ctx, base+".jar.asc", signature)

		artifacts := []v2alpha1.Artifact{{Extension: "jar"}, {Extension: "pom"}}
		src := mavenResource(rs.hostURL(), group, artifact, version, artifacts...)
		archive, err := repo.DownloadResource(ctx, src, deployCredentials())
		require.NoError(t, err)

		dst := mavenResource(rs.hostURLFor(transferRepoName), group, artifact, version, artifacts...)
		_, err = repo.UploadResource(ctx, dst, archive, deployCredentials())
		require.NoError(t, err)

		code, out = mc.mvn(t, ctx, "-C", "dependency:get",
			"-DremoteRepositories=it::::"+rs.containerURLFor(transferRepoName),
			"-Dartifact="+fmt.Sprintf("%s:%s:%s", group, artifact, version),
			"-Dtransitive=false",
		)
		require.Equalf(t, 0, code, "mvn dependency:get from the target repository failed:\n%s", out)

		got, err := repo.DownloadResource(ctx, dst, deployCredentials())
		require.NoError(t, err)
		entries := readTgzEntries(t, got)
		assert.Equal(t, artifactBytes, entries[artifact+"-"+version+".jar"])
		assert.Equal(t, signature, entries[artifact+"-"+version+".jar.asc"])
		assert.Contains(t, entries, artifact+"-"+version+".pom")
	})

	// Direction 5: a detached PGP signature next to the artifact is fetched
	// without being listed in the spec, and turns a single-file download into
	// a tgz holding the file and its signature.
	t.Run("download picks up the .asc signature automatically", func(t *testing.T) {
		const (
			group    = "com.example"
			artifact = "signed"
			version  = "5.0.0"
		)
		artifactBytes := []byte("signed-payload")
		signature := []byte("-----BEGIN PGP SIGNATURE-----\nfake\n-----END PGP SIGNATURE-----\n")

		res := mavenResource(rs.hostURL(), group, artifact, version)
		_, err := repo.UploadResource(ctx, res, archiveOf(t, artifact+"-"+version+".jar", artifactBytes), deployCredentials())
		require.NoError(t, err)
		base := fmt.Sprintf("%s/%s/%s/%s-%s",
			strings.ReplaceAll(group, ".", "/"), artifact, version, artifact, version)
		rs.writeFile(t, ctx, base+".jar.asc", signature)

		b, err := repo.DownloadResource(ctx, res, deployCredentials())
		require.NoError(t, err)
		mt, ok := b.(blob.MediaTypeAware).MediaType()
		require.True(t, ok)
		assert.Equal(t, "application/x-tgz", mt)
		entries := readTgzEntries(t, b)
		require.Contains(t, entries, artifact+"-"+version+".jar.sha1", "checksum siblings ride along with the signature")
		assert.Equal(t, artifactBytes, entries[artifact+"-"+version+".jar"])
		assert.Equal(t, signature, entries[artifact+"-"+version+".jar.asc"])
	})

	// Direction 4: sibling files are mirrored as served, never verified.
	t.Run("download mirrors checksum siblings verbatim without verifying them", func(t *testing.T) {
		// Our upload deploys the checksum siblings the archive carries; the
		// download brings them back next to the file.
		okBytes := []byte("verify-me")
		okRes := mavenResource(rs.hostURL(), "com.example", "verified", "4.0.0")
		_, err := repo.UploadResource(ctx, okRes, archiveOf(t, "verified-4.0.0.jar", okBytes), deployCredentials())
		require.NoError(t, err)
		entries := readTgzEntries(t, mustDownload(t, ctx, repo, okRes))
		assert.Equal(t, okBytes, entries["verified-4.0.0.jar"])
		assert.Equal(t, fmt.Sprintf("%x", sha1.Sum(okBytes)), string(entries["verified-4.0.0.jar.sha1"]))

		// A checksum that does not match its file (Nexus stores an uploaded
		// checksum file verbatim) is still mirrored as-is: the download neither
		// fails nor repairs it. Verification is the consumer's job. A distinct
		// GAV is required because the allow-once write policy rejects
		// overwriting a release file (HTTP 409).
		const (
			group    = "com.example"
			artifact = "corrupted"
			version  = "4.0.1"
		)
		base := fmt.Sprintf("%s/%s/%s/%s-%s",
			strings.ReplaceAll(group, ".", "/"), artifact, version, artifact, version)
		wrong := []byte("0000000000000000000000000000000000000000")
		rs.writeFile(t, ctx, base+".jar", []byte("corrupt-me"))
		rs.writeFile(t, ctx, base+".jar.sha1", wrong)

		entries = readTgzEntries(t, mustDownload(t, ctx, repo, mavenResource(rs.hostURL(), group, artifact, version)))
		assert.Equal(t, []byte("corrupt-me"), entries[artifact+"-"+version+".jar"])
		assert.Equal(t, wrong, entries[artifact+"-"+version+".jar.sha1"])
	})

	// Nexus takes one to two minutes to boot, so the snapshot scenario shares
	// the containers instead of starting its own.
	t.Run("snapshot multi-file download", func(t *testing.T) {
		snapshotMultiFileTgz(t, ctx, rs, mc, repo)
	})
}

// snapshotMultiFileTgz exercises the full snapshot
// multi-file download path against a REAL `mvn deploy:deploy-file` and a real
// Nexus server: deploy a -SNAPSHOT main jar together with sources and
// javadoc classifier jars in a single Maven invocation (as `mvn deploy` would
// produce for a project with the source/javadoc plugins bound), then resolve
// all three with an explicit artifacts list and assert the result is a tgz
// containing exactly those three jars under their timestamped names. The
// version-level maven-metadata.xml that `mvn deploy:deploy-file` writes on a
// SNAPSHOT deploy is what supplies those names (see Client.Resolve).
func snapshotMultiFileTgz(t *testing.T, ctx context.Context, rs *repoServer, mc *mavenContainer, repo *resource.ResourceRepository) {
	t.Helper()

	const (
		group    = "com.example"
		artifact = "lib"
		version  = "1.0-SNAPSHOT"
	)
	mainBytes := []byte("main-jar-payload")
	sourcesBytes := []byte("sources-jar-payload")
	javadocBytes := []byte("javadoc-jar-payload")

	require.NoError(t, mc.c.CopyToContainer(ctx, mainBytes, "/work/lib.jar", 0o644))
	require.NoError(t, mc.c.CopyToContainer(ctx, sourcesBytes, "/work/lib-sources.jar", 0o644))
	require.NoError(t, mc.c.CopyToContainer(ctx, javadocBytes, "/work/lib-javadoc.jar", 0o644))

	// A single deploy-file invocation with -Dsources/-Djavadoc publishes the
	// main artifact plus the sources and javadoc classifier jars in one go
	// (deploy-file supports both flags directly), exactly like a real `mvn
	// deploy` of a project with the source/javadoc plugins bound. Because the
	// version is a SNAPSHOT, this also writes timestamped filenames and the
	// version-level maven-metadata.xml that pattern resolution depends on.
	code, out := mc.mvn(t, ctx, "deploy:deploy-file",
		"-Dfile=/work/lib.jar",
		"-Dsources=/work/lib-sources.jar",
		"-Djavadoc=/work/lib-javadoc.jar",
		"-DgroupId="+group,
		"-DartifactId="+artifact,
		"-Dversion="+version,
		"-Dpackaging=jar",
		"-DrepositoryId=it",
		"-Durl="+rs.containerURL(),
	)
	require.Equalf(t, 0, code, "mvn deploy:deploy-file (snapshot + sources + javadoc) failed:\n%s", out)

	// Every entry resolves its timestamped file name from the version-level
	// maven-metadata.xml maven just wrote.
	res := mavenResource(rs.hostURL(), group, artifact, version,
		v2alpha1.Artifact{Extension: "jar"},
		v2alpha1.Artifact{Extension: "jar", Classifier: "sources"},
		v2alpha1.Artifact{Extension: "jar", Classifier: "javadoc"},
	)
	b, err := repo.DownloadResource(ctx, res, deployCredentials())
	require.NoError(t, err)

	mtAware, ok := b.(blob.MediaTypeAware)
	require.True(t, ok, "downloaded blob must implement blob.MediaTypeAware")
	mt, ok := mtAware.MediaType()
	require.True(t, ok, "downloaded multi-file blob must report a media type")
	assert.Equal(t, "application/x-tgz", mt)

	entries := readTgzEntries(t, b)
	jars := 0
	for name := range entries {
		if strings.HasSuffix(name, ".jar") {
			jars++
		}
	}
	require.Equalf(t, 3, jars, "expected main+sources+javadoc jars, got entries: %v", entryNames(entries))

	var mainName, sourcesName, javadocName string
	for name := range entries {
		switch {
		case strings.HasSuffix(name, "-sources.jar"):
			sourcesName = name
		case strings.HasSuffix(name, "-javadoc.jar"):
			javadocName = name
		case strings.HasSuffix(name, ".jar"):
			mainName = name
		}
	}
	require.NotEmptyf(t, mainName, "main jar entry not found in tar: %v", entryNames(entries))
	require.NotEmptyf(t, sourcesName, "sources jar entry not found in tar: %v", entryNames(entries))
	require.NotEmptyf(t, javadocName, "javadoc jar entry not found in tar: %v", entryNames(entries))

	assert.Equal(t, mainBytes, entries[mainName], "main jar content mismatch")
	assert.Equal(t, sourcesBytes, entries[sourcesName], "sources jar content mismatch")
	assert.Equal(t, javadocBytes, entries[javadocName], "javadoc jar content mismatch")

	// The tar entry names embed the timestamped snapshot version (from
	// maven-metadata.xml), never the literal "-SNAPSHOT" version.
	assert.NotContains(t, mainName, "SNAPSHOT", "tar entry name must use the timestamped version, not literal SNAPSHOT")
	assert.Contains(t, mainName, artifact+"-"+strings.TrimSuffix(version, "-SNAPSHOT"))

	// mvn deploy writes .sha1 and .md5 next to every file; they ride along
	// unchanged.
	assert.Contains(t, entries, mainName+".sha1")
	assert.Contains(t, entries, mainName+".md5")
}

// readTgzEntries reads a gzip-compressed tar blob and returns a map of entry
// name to its raw content.
func readTgzEntries(t *testing.T, b interface {
	ReadCloser() (io.ReadCloser, error)
}) map[string][]byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer rc.Close()
	gz, err := gzip.NewReader(rc)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	entries := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(tr)
		require.NoError(t, err)
		entries[h.Name] = data
	}
	return entries
}

// entryNames renders the keys of a tar-entries map for diagnostic output.
func entryNames(entries map[string][]byte) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	return names
}

// mavenResource builds a resource for group:artifact:version at repoURL. With
// no artifacts given it addresses the main jar.
func mavenResource(repoURL, group, artifact, version string, artifacts ...v2alpha1.Artifact) *descriptor.Resource {
	if len(artifacts) == 0 {
		artifacts = []v2alpha1.Artifact{{Extension: "jar"}}
	}
	return &descriptor.Resource{Access: &v2alpha1.Maven{
		Type:       runtime.NewVersionedType(v2alpha1.Type, v2alpha1.Version),
		RepoURL:    repoURL,
		GroupID:    group,
		ArtifactID: artifact,
		Version:    version,
		Artifacts:  artifacts,
	}}
}

// archiveOf packs one file with its .sha1 and .md5 siblings into the
// application/x-tgz shape UploadResource expects, the same shape a download
// from a repository that publishes checksums produces.
func archiveOf(t *testing.T, name string, data []byte) blob.ReadOnlyBlob {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range []struct {
		name string
		data []byte
	}{
		{name, data},
		{name + ".sha1", []byte(fmt.Sprintf("%x", sha1.Sum(data)))},
		{name + ".md5", []byte(fmt.Sprintf("%x", md5.Sum(data)))},
	} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: e.name, Mode: 0o644, Size: int64(len(e.data))}))
		_, err := tw.Write(e.data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return inmemory.New(bytes.NewReader(buf.Bytes()), inmemory.WithMediaType("application/x-tgz"))
}

// mustDownload downloads res and fails the test on error.
func mustDownload(t *testing.T, ctx context.Context, repo *resource.ResourceRepository, res *descriptor.Resource) blob.ReadOnlyBlob {
	t.Helper()
	b, err := repo.DownloadResource(ctx, res, deployCredentials())
	require.NoError(t, err)
	return b
}

// entryOf asserts that the downloaded archive is a tgz holding an entry with
// the given name and returns its content. Sibling files ride along, so the
// archive may hold more entries than the listed files.
func entryOf(t *testing.T, b blob.ReadOnlyBlob, name string) []byte {
	t.Helper()
	mt, ok := b.(blob.MediaTypeAware).MediaType()
	require.True(t, ok)
	require.Equal(t, "application/x-tgz", mt)
	entries := readTgzEntries(t, b)
	data, ok := entries[name]
	require.Truef(t, ok, "entry %q not found in %v", name, entryNames(entries))
	return data
}
