package internal

// Copy mode constants (mirrors transfer.CopyMode values).
const (
	CopyModeLocalBlobResources = 0
	CopyModeAllResources       = 1
)

// Upload type constants (mirrors transfer.UploadType values).
const (
	UploadAsDefault     = 0
	UploadAsLocalBlob   = 1
	UploadAsOciArtifact = 2
)
