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
	"path/filepath"

	"github.com/nlepage/go-tarfs"
	"github.com/opencontainers/go-digest"
	ociimagespec "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
)

// ArtifactSetFormat is the format of an ArtifactSet.
// It describes the layout and characteristics of the artifact set, as in old OCM there can be multiple
// versions stored.
type ArtifactSetFormat int

const (
	// ArtifactSetFormatLegacyOCI is the previous OCI format adopted by the legacy OCM library.
	// Characteristics:
	// - index stored as [ociimagespec.ImageIndexFile], but with schemaVersion 2 and optional mediaTypes
	// - blobs stored as flat files in the blobs directory (blobs/algo.encoded)
	// - can have encoded OCI Image Layout
	ArtifactSetFormatLegacyOCI ArtifactSetFormat = iota
	// ArtifactSetFormatOCM is the old OCM format adopted by the OCM library.
	// Characteristics:
	// - index stored as ArtifactSetDescriptorFileName, but with schemaVersion 2 and without mediaTypes
	// - blobs stored as flat files in the blobs directory (blobs/algo.encoded)
	ArtifactSetFormatOCM

	// ArtifactSetFormatOCI is a new OCI format adopted by the OCM library after the reference library rework.
	// Characteristics:
	// - index stored as [ociimagespec.ImageIndexFile], with schemaVersion 2 and mediaTypes
	// - blobs stored as files in the blobs directory by algorithm (blobs/algo/encoded)
	// - must include an OCI Image Layout file.
	//
	// This is effectively the same format as a distribution spec OCI Image Layout.
	// If one must use an artifact set, it is recommended that it be stored in this format.
	//
	// Note that when working with ocm CLI / library < 0.35, you will likely encounter either
	// ArtifactSetFormatOCM, or ArtifactSetFormatLegacyOCI.
	ArtifactSetFormatOCI
)

// ArtifactSetDescriptorFileName is the file name for artifact index if the artifact set is created
// with ArtifactSetFormatOCM.
const ArtifactSetDescriptorFileName = "artifact-descriptor.json"

// SynthesizedBlobFormatSuffix is the suffix for the synthesized blob format.
// This is the format to distinguish between artifact sets and regular manifests.
const SynthesizedBlobFormatSuffix = "+tar+gzip"

var recognizedArtifactSetMediaTypes = map[string]bool{
	"application/vnd.oci.image.manifest.v1" + SynthesizedBlobFormatSuffix: true,
	"application/vnd.oci.image.index.v1" + SynthesizedBlobFormatSuffix:    true,
	// TODO(jakobmoellerdev): technically docker image types could be an artifact set too, but
	//   in the new world its probably not worth supporting as docker has started support for OCI too.
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
		if mt, known := mtAware.MediaType(); known && IsArtifactSetMediaType(mt) {
			return nil, fmt.Errorf("unsupported media type %q, expected one of %v", mt, recognizedArtifactSetMediaTypes)
		}
	}

	raw, err := b.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("unable to open blob: %w", err)
	}

	// Read the first 512 bytes (Tar header size)
	buf := make([]byte, 512)
	n, err := raw.Read(buf)
	if err != nil {
		return nil, err
	}

	var reader io.Reader = raw

	closeFn := raw.Close
	// Check for Gzip magic number (0x1F, 0x8B)
	if n >= 2 && buf[0] == 0x1F && buf[1] == 0x8B {
		multiReader := io.MultiReader(bytes.NewReader(buf), raw)
		gzipReader, err := gzip.NewReader(multiReader)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		closeFn = func() error {
			return errors.Join(gzipReader.Close(), raw.Close())
		}
		reader = gzipReader
	}

	fs, err := tarfs.New(reader)
	if err != nil {
		return nil, fmt.Errorf("unable to create tarfs: %w", err)
	}
	fileSystem := fs.(fileSystem)

	idx, err := NewArtifactSetIndex(fs)
	if err != nil {
		return nil, fmt.Errorf("unable to read index.json: %w", err)
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
			Name: filepath.Join(BlobsDirectoryName, dig.Algorithm().String(), dig.Encoded()),
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
				algoDir, err := a.fs.ReadDir(filepath.Join(BlobsDirectoryName, entry.Name()))
				if err != nil {
					return nil, fmt.Errorf("unable to list blobs: %w", err)
				}
				algo := filepath.Base(entry.Name())
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
	var name string
	switch format {
	case ArtifactSetFormatOCM, ArtifactSetFormatLegacyOCI:
		file, err := ToBlobFileName(dig)
		if err != nil {
			return nil, err
		}
		name = filepath.Join(BlobsDirectoryName, file)
	case ArtifactSetFormatOCI:
		dig := digest.Digest(dig)
		name = filepath.Join(BlobsDirectoryName, dig.Algorithm().String(), dig.Encoded())
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
		if algoDir, err := filesystem.Open(filepath.Join(BlobsDirectoryName, algo.String())); err == nil {
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
