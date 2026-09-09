// Package repository implements read-only Git resource access and digest processing.
// Downloads return file-backed TGZ blobs. Close each blob after use to remove its file.
// A supplied commit takes precedence over the informational ref.
package repository
