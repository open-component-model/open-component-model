package filesystem

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
)

// TarWriter writes filesystem entries in caller-supplied order using the same
// headers and filtering as GetBlobFromPath. It does not own the output writer.
type TarWriter struct {
	writer  *tar.Writer
	options DirOptions
}

// NewTarWriter creates an uncompressed archive writer. Only the entry options
// (Reproducible, PreserveSymlinks, PreserveDir, OmitRoot, OmitDirTrailingSlash,
// IncludePatterns and ExcludePatterns) apply; blob and host-path options are ignored.
func NewTarWriter(w io.Writer, opt DirOptions) *TarWriter {
	return &TarWriter{writer: tar.NewWriter(w), options: opt}
}

// WriteEntry writes an entry named by a slash-separated filesystem path. The
// caller supplies metadata without following symlinks, and must skip descendants
// when a directory returns fs.SkipDir. File content is opened only for included
// regular entries; symlinks require the optional ReadlinkFS capability.
func (w *TarWriter) WriteEntry(ctx context.Context, name string, info fs.FileInfo, source fs.FS) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		if !w.options.PreserveSymlinks {
			return fmt.Errorf("symlinks are not supported yet: found symlink %q", name)
		}
		return processSymlink(name, info, source, w.options, w.writer)
	}
	if info.IsDir() {
		return processDirectory(name, info, w.options, w.writer, name == ".")
	}
	return processFile(name, info, source, w.options, w.writer)
}

// Close finishes the archive without closing the underlying writer.
func (w *TarWriter) Close() error { return w.writer.Close() }
