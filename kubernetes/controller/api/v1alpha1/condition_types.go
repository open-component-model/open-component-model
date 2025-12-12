package v1alpha1

const (
	// ConfigureContextFailedReason is used when the controller failed to create an authenticated context.
	ConfigureContextFailedReason = "ConfigureContextFailed"

	// CheckVersionFailedReason is used when the controller failed to check for new versions.
	CheckVersionFailedReason = "CheckVersionFailed"

	// ResourceIsNotAvailable is used when the referenced resource is not available.
	ResourceIsNotAvailable = "ResourceIsNotAvailable"

	// ReplicationFailedReason is used when the referenced component is not Ready yet.
	ReplicationFailedReason = "ReplicationFailed"

	// GetRepositoryFailedReason is used when the OCM repository cannot be fetched.
	GetRepositoryFailedReason = "GetRepositoryFailed"

	// GetComponentVersionFailedReason is used when the component cannot be fetched.
	GetComponentVersionFailedReason = "GetComponentVersionFailed"

	// GetOCMResourceFailedReason is used when the OCM resource cannot be fetched.
	GetOCMResourceFailedReason = "GetOCMResourceFailed"

	// MarshalFailedReason is used when we fail to marshal a struct.
	MarshalFailedReason = "MarshalFailed"

	// ApplyFailed is used when we fail to create or update a resource.
	ApplyFailed = "ApplyFailed"

	// GetReferenceFailedReason is used when we fail to get a reference.
	GetReferenceFailedReason = "GetReferenceFailed"

	// GetResourceFailedReason is used when we fail to get the resource.
	GetResourceFailedReason = "GetResourceFailed"

	// StatusSetFailedReason is used when we fail to set the component status.
	StatusSetFailedReason = "StatusSetFailed"

	// DeletionFailedReason is used when we fail to delete the resource.
	DeletionFailedReason = "DeletionFailed"

	// ResourceNotSynced is used when the referenced resource is not yet synced.
	ResourceNotSynced = "ResourceNotSynced"

	// ResolutionInProgress is used when resolution is still in progress.
	ResolutionInProgress = "ResolutionInProgress"
)
