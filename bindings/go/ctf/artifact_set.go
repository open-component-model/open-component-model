package ctf

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"

	"github.com/nlepage/go-tarfs"
	"github.com/opencontainers/go-digest"
	ociimagespec "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
)

// ArtifactSetFormat is the format of an ArtifactSet.
// The OCM library (https://github.com/open-component-model/ocm, package
// api/oci/extensions/repositories/artifactset) writes artifact sets in one of three
// structure formats, all of which are supported for reading. In every format, each
// index entry carries the mediaType and size of the referenced manifest or index;
// NewArtifactSetIndex rejects entries without either.
type ArtifactSetFormat int

const (
	// ArtifactSetFormatLegacyOCI corresponds to the OCM library format "oci/v1",
	// the default since OCM library v0.1.0. Every artifact set the OCM library
	// synthesizes for local blobs (e.g. from ociArtifact, dockermulti or docker
	// inputs, or OCI resources transferred by value) has this format.
	// Characteristics:
	// - index stored as [ociimagespec.ImageIndexFile]
	// - [ociimagespec.ImageLayoutFile] present
	// - blobs stored as flat files in the blobs directory (blobs/algo.encoded)
	ArtifactSetFormatLegacyOCI ArtifactSetFormat = iota
	// ArtifactSetFormatOCM corresponds to the OCM library format "ocm/v1". It was never
	// the default; the OCM library only writes it on request. Pre-release versions (up to
	// late 2022, including v0.0.0) wrote the same layout with the index named
	// "artefact-descriptor.json", which is not supported.
	// Characteristics:
	// - index stored as ArtifactSetDescriptorFileName
	// - no [ociimagespec.ImageLayoutFile]
	// - blobs stored as flat files in the blobs directory (blobs/algo.encoded)
	ArtifactSetFormatOCM

	// ArtifactSetFormatOCI corresponds to the OCM library format "oci/v1+compliant",
	// added in OCM library v0.36 and only written by downloads with --oci-layout. It
	// reaches a component version only if such a download is added as a local blob
	// with an artifact set media type.
	// Characteristics:
	// - index stored as [ociimagespec.ImageIndexFile]
	// - [ociimagespec.ImageLayoutFile] present
	// - blobs stored as files in the blobs directory by algorithm (blobs/algo/encoded)
	//
	// This is effectively an OCI Image Layout.
	ArtifactSetFormatOCI
)

func (f ArtifactSetFormat) String() string {
	switch f {
	case ArtifactSetFormatLegacyOCI:
		return "LegacyOCI"
	case ArtifactSetFormatOCM:
		return "OCM"
	case ArtifactSetFormatOCI:
		return "OCI"
	default:
		return fmt.Sprintf("ArtifactSetFormat(%d)", int(f))
	}
}

// ArtifactSetDescriptorFileName is the file name for artifact index if the artifact set is created
// with ArtifactSetFormatOCM.
const ArtifactSetDescriptorFileName = "artifact-descriptor.json"

// SynthesizedBlobFormatSuffix is the suffix for the synthesized blob format.
// This is the format to distinguish between artifact sets and regular manifests.
const SynthesizedBlobFormatSuffix = "+tar+gzip"

var recognizedArtifactSetMediaTypes = map[string]bool{
	"application/vnd.oci.image.manifest.v1" + SynthesizedBlobFormatSuffix: true,
	"application/vnd.oci.image.index.v1" + SynthesizedBlobFormatSuffix:    true,
	// Docker image types are artifact sets too: the OCM library synthesizes them for
	// docker/dockermulti inputs and OCI resources whose manifests use the docker
	// media types. Reading and uploading them must preserve the manifest digest, so
	// they are read as-is (no docker->OCI conversion), same as the OCI variants above.
	"application/vnd.docker.distribution.manifest.v2" + SynthesizedBlobFormatSuffix:      true,
	"application/vnd.docker.distribution.manifest.list.v2" + SynthesizedBlobFormatSuffix: true,
}

// IsArtifactSetMediaType gives confirmation that the given media type is likely containing
// an old OCM Format Style artifact set.
func IsArtifactSetMediaType(mt string) bool {
	return recognizedArtifactSetMediaTypes[mt]
}

// ArtifactSet is the equivalent (deprecated) implementation of
// https://github.com/open-component-model/ocm/tree/2091216b223a5c084895cf501a0570a4de485c09/api/oci/extensions/repositories/artifactset
//
// NOTE: Even though it does look similar to an OCI Image Layout, it is not
// the same. Notable differences are:
//
//   - The blobs directory orders blobs in the form
//
//     blobs/digest-algo.digest
//
//     instead of
//
//     blobs/digest-algo/digest.
//
//   - The index.json file IS a valid OCI Image Index json, but does not maintain
//     the ociimagespec.AnnotationRefName correctly, instead it sets it to the version
//     of the resource. Additionally, it maintains the software.ocm/tags annotation.
//     This means it is MANDATORY to understand the resource access.referenceName
//     in place of the usual meaning of the refName to fully target an OCI Image out
//     of an ArtifactSet.
//
//   - The index.json file contains a software.ocm/main annotation, that declares
//     the main blob to be introspected (useful for single-layer artifacts or multi-layer
//     artifacts with one main layer and multiple metadata layers).
//
// Altogether, this makes the ArtifactSet a custom format that is not compatible
// with OCI Image Layouts or CTF readings that are unaware of the Component Descriptor.
//
// It is thus deprecated and should not be used anymore.
//
// This ArtifactSetMediaType now only serves to read from old LocalBlobs and CTFs and is maintained
// to ensure compatibility.
//
// New CTFs should and will always include localBlobs not as artifact sets, but as proper
// OCI Image Layouts.
type ArtifactSet struct {
	close func() error
	fs    fileSystem
	idx   *ArtifactSetIndex
}

var (
	_ io.Closer         = (*ArtifactSet)(nil)
	_ ReadOnlyBlobStore = (*ArtifactSet)(nil)
)

// NewArtifactSetFromBlob creates a new ArtifactSet from a blob.
// It will start owning the readcloser in the blob and needs to be closed due to this.
// It is backed by a tar filesystem that is either read from a tar file or a gzip file.
// The gzip format is not detected by ArtifactSetMediaType, but by the magic number in the first 512 bytes of the file.
// The blob must be a valid ArtifactSet, otherwise an error is returned.
//
// The ArtifactSet merely supports read-only and any future write operation is no longer supported.
// All ArtifactSet's encountered in the wild MUST be converted to OCI Image Layouts.
//
// See ArtifactSet for more information about the format.
func NewArtifactSetFromBlob(b blob.ReadOnlyBlob) (*ArtifactSet, error) {
	// if we are media type aware, we need to check the media type.
	// otherwise we do a best effort to detect the media type.
	if mtAware, ok := b.(blob.MediaTypeAware); ok {
		if mt, known := mtAware.MediaType(); known && !IsArtifactSetMediaType(mt) {
			return nil, fmt.Errorf("unsupported media type %q, expected one of %v", mt, recognizedArtifactSetMediaTypes)
		}
	}

	raw, err := b.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("unable to open blob: %w", err)
	}

	// Read the first two bytes for gzip detection and replay them afterwards.
	var header [2]byte
	n, err := io.ReadFull(raw, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, errors.Join(fmt.Errorf("unable to read blob: %w", err), raw.Close())
	}
	reader := io.MultiReader(bytes.NewReader(header[:n]), raw)
	if sizeAware, ok := b.(blob.SizeAware); ok {
		if size := sizeAware.Size(); size > blob.SizeUnknown {
			reader = io.LimitReader(reader, size)
		}
	}

	closeFn := raw.Close
	// Check for Gzip magic number (0x1F, 0x8B)
	if n == 2 && header[0] == 0x1F && header[1] == 0x8B {
		gzipReader, err := gzip.NewReader(reader)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("failed to create gzip reader: %w", err), raw.Close())
		}
		closeFn = func() error {
			return errors.Join(gzipReader.Close(), raw.Close())
		}
		reader = gzipReader
	}

	fs, err := tarfs.New(reader)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("unable to create tarfs: %w", err), closeFn())
	}
	fileSystem := fs.(fileSystem)

	idx, err := NewArtifactSetIndex(fs)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("unable to read index.json: %w", err), closeFn())
	}

	return &ArtifactSet{
		close: closeFn,
		fs:    fileSystem,
		idx:   idx,
	}, nil
}

// ConvertToOCIImageLayout converts an ArtifactSet to an OCI Image Layout in tar format.
// It will write the index.json and all blobs to the writer.
// It converts old blobs according to a given manifestNameFn.
//
// The manifestNameFn is used to convert the old name of the blob to the new name.
// Example:
//
//	func manifestNameFn(digest digest.Digest, oldName string) (string, error) {
//		return fmt.Sprintf("ghcr.io/open-component-model/%s", digest), nil
//	}
//
// This is needed due to the lossy typing that requires the component descriptor (see ArtifactSet for information).
func ConvertToOCIImageLayout(ctx context.Context, as *ArtifactSet, writer io.Writer, manifestNameFn func(ctx context.Context, digest digest.Digest, oldName string) (string, error)) (err error) {
	tw := tar.NewWriter(writer)
	defer func() {
		err = errors.Join(err, tw.Close())
	}()

	layout := ociimagespec.ImageLayout{Version: ociimagespec.ImageLayoutVersion}
	layoutRaw, err := json.Marshal(layout)
	if err != nil {
		return fmt.Errorf("unable to marshal layout: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: ociimagespec.ImageLayoutFile,
		Size: int64(len(layoutRaw)),
	}); err != nil {
		return fmt.Errorf("unable to write layout header: %w", err)
	}
	if _, err := tw.Write(layoutRaw); err != nil {
		return fmt.Errorf("unable to write layout: %w", err)
	}

	idx := as.GetIndex()
	for _, manifest := range idx.Manifests {
		annotations := manifest.Annotations
		if annotations == nil {
			continue
		}
		name, ok := annotations[ociimagespec.AnnotationRefName]
		if !ok {
			continue
		}
		name, err = manifestNameFn(ctx, manifest.Digest, name)
		if err != nil {
			return fmt.Errorf("unable to generate manifest name: %w", err)
		}
		annotations[ociimagespec.AnnotationRefName] = name
	}
	idxJson, err := json.Marshal(idx)
	if err != nil {
		return fmt.Errorf("unable to marshal index.json: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: ociimagespec.ImageIndexFile,
		Size: int64(len(idxJson)),
	}); err != nil {
		return fmt.Errorf("unable to write index.json header: %w", err)
	}
	if _, err := tw.Write(idxJson); err != nil {
		return fmt.Errorf("unable to write index.json: %w", err)
	}

	blobs, err := as.ListBlobs(ctx)
	if err != nil {
		return fmt.Errorf("unable to list blobs: %w", err)
	}

	for _, b := range blobs {
		dig, err := digest.Parse(b)
		if err != nil {
			return fmt.Errorf("unable to parse digest %s: %w", b, err)
		}
		b, err := as.GetBlob(ctx, b)
		if err != nil {
			return fmt.Errorf("unable to get blob %s: %w", b, err)
		}
		bsizeAware, ok := b.(blob.SizeAware)
		if !ok {
			return fmt.Errorf("blob %s does not have a Size", b)
		}

		if err := tw.WriteHeader(&tar.Header{
			Name: path.Join(BlobsDirectoryName, dig.Algorithm().String(), dig.Encoded()),
			Size: bsizeAware.Size(),
		}); err != nil {
			return fmt.Errorf("unable to write header for blob %s: %w", b, err)
		}

		rc, err := b.ReadCloser()
		if err != nil {
			return fmt.Errorf("unable to read blob %s: %w", b, err)
		}
		if _, err := io.Copy(tw, rc); err != nil {
			return fmt.Errorf("unable to copy blob %s: %w", b, err)
		}
	}

	return nil
}

// fileSystem is a subset of the tarfs interface that is used to
// access the tar underneath with Stat and ReadDir.
// This is needed because there is no such interface available from tarfs directly
// and tarfs itself only exposes the fs.FS interface even though it does support
// ReadDir and Stat.
type fileSystem interface {
	fs.StatFS
	fs.ReadDirFS
}

func (a *ArtifactSet) GetIndex() ociimagespec.Index {
	return a.idx.Index
}

func (a *ArtifactSet) ListBlobs(ctx context.Context) (digests []string, err error) {
	dir, err := a.fs.ReadDir(BlobsDirectoryName)
	if err != nil {
		return nil, fmt.Errorf("unable to list blobs: %w", err)
	}

	digests = make([]string, 0, len(dir))
	switch a.idx.Format {
	case ArtifactSetFormatLegacyOCI, ArtifactSetFormatOCM:
		for _, entry := range dir {
			if entry.Type().IsRegular() {
				digests = append(digests, ToDigest(entry.Name()))
			}
		}
	case ArtifactSetFormatOCI:
		for _, entry := range dir {
			if entry.IsDir() {
				algoDir, err := a.fs.ReadDir(path.Join(BlobsDirectoryName, entry.Name()))
				if err != nil {
					return nil, fmt.Errorf("unable to list blobs: %w", err)
				}
				algo := path.Base(entry.Name())
				for _, entry := range algoDir {
					if entry.Type().IsRegular() {
						digests = append(digests, algo+":"+entry.Name())
					}
				}
			}
		}
	}
	return digests, nil
}

func (a *ArtifactSet) GetBlob(ctx context.Context, digest string) (blob.ReadOnlyBlob, error) {
	return newArtifactBlob(a.fs, digest, a.idx.Format)
}

func (a *ArtifactSet) Close() error {
	return a.close()
}

func (a *ArtifactSet) Format() ArtifactSetFormat {
	return a.idx.Format
}

// ArtifactBlob is a blob.ReadOnlyBlob that is backed by an ArtifactSet.
type ArtifactBlob struct {
	fs     fileSystem
	name   string // name of the blob
	digest string
	size   int64
}

var (
	_ blob.ReadOnlyBlob = (*ArtifactBlob)(nil)
	_ blob.DigestAware  = (*ArtifactBlob)(nil)
	_ blob.SizeAware    = (*ArtifactBlob)(nil)
)

func newArtifactBlob(fs fileSystem, dig string, format ArtifactSetFormat) (blob.ReadOnlyBlob, error) {
	name, err := artifactSetBlobPath(dig, format)
	if err != nil {
		return nil, err
	}

	f, err := fs.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("unable to stat file %q: %w", name, err)
	}
	return &ArtifactBlob{
		name:   name,
		fs:     fs,
		digest: dig,
		size:   f.Size(),
	}, nil
}

// artifactSetBlobPath returns the path of the blob with the given digest within
// an artifact set of the given format.
func artifactSetBlobPath(dig string, format ArtifactSetFormat) (string, error) {
	if format == ArtifactSetFormatOCI {
		d, err := digest.Parse(dig)
		if err != nil {
			return "", fmt.Errorf("invalid digest %q: %w", dig, err)
		}
		return path.Join(BlobsDirectoryName, d.Algorithm().String(), d.Encoded()), nil
	}
	file, err := ToBlobFileName(dig)
	if err != nil {
		return "", err
	}
	return path.Join(BlobsDirectoryName, file), nil
}

func (a *ArtifactBlob) Size() (size int64) {
	return a.size
}

func (a *ArtifactBlob) Digest() (digest string, known bool) {
	return a.digest, true
}

func (a *ArtifactBlob) ReadCloser() (io.ReadCloser, error) {
	return a.fs.Open(a.name)
}

// ArtifactSetIndex is a subset of the ociimagespec.Index that is used to
// access the index.json underneath, with context of the actual underlying OCM artifact set Format.
type ArtifactSetIndex struct {
	ociimagespec.Index
	Format ArtifactSetFormat
}

// NewArtifactSetIndex reads the index of an artifact set and determines its format.
// It fails if an index entry has no media type or size.
func NewArtifactSetIndex(filesystem fs.FS) (*ArtifactSetIndex, error) {
	var format ArtifactSetFormat
	base, err := decodeIndexFromFS(filesystem, ociimagespec.ImageIndexFile)
	if err != nil {
		var descriptorErr error
		if base, descriptorErr = decodeIndexFromFS(filesystem, ArtifactSetDescriptorFileName); descriptorErr != nil {
			return nil, fmt.Errorf(
				"unable to read artifact set index from image index (%w) or artifact set descriptor (%w)",
				err, descriptorErr)
		}
		format = ArtifactSetFormatOCM
	} else {
		format = determineArtifactSetOCIFormat(filesystem)
	}

	// Without a media type, OCI tooling cannot walk the graph below the entry, and
	// without a size it cannot verify the entry. Every OCM library version records
	// both, so a missing one means a corrupt set.
	for _, manifest := range base.Manifests {
		if manifest.MediaType == "" {
			return nil, fmt.Errorf("artifact set index entry %s has no media type", manifest.Digest)
		}
		if manifest.Size <= 0 {
			return nil, fmt.Errorf("artifact set index entry %s has no size", manifest.Digest)
		}
	}

	return &ArtifactSetIndex{
		Index:  *base,
		Format: format,
	}, nil
}

func determineArtifactSetOCIFormat(filesystem fs.FS) ArtifactSetFormat {
	for _, algo := range []digest.Algorithm{
		digest.SHA256,
		digest.SHA512,
	} {
		// the subdirectory for algos is unique to new generation blob directories and we can deduce the
		// new OCI artifact set format from that.
		if algoDir, err := filesystem.Open(path.Join(BlobsDirectoryName, algo.String())); err == nil {
			_ = algoDir.Close()
			return ArtifactSetFormatOCI
		}
	}

	return ArtifactSetFormatLegacyOCI
}

func decodeIndexFromFS(fs fs.FS, name string) (*ociimagespec.Index, error) {
	idx := ociimagespec.Index{}
	rawidx, err := fs.Open(name)
	if err != nil {
		return nil, fmt.Errorf("unable to open %s: %w", name, err)
	}
	defer rawidx.Close()
	if err := json.NewDecoder(rawidx).Decode(&idx); err != nil {
		return nil, fmt.Errorf("unable to decode %s: %w", name, err)
	}
	return &idx, err
}
