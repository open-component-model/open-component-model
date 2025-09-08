package v1alpha1

// Ocm credential config key for secrets.
const (
	// OCMCredentialConfigKey defines the secret key to look for in case a user provides an ocm credential config.
	OCMCredentialConfigKey = ".ocmcredentialconfig" //nolint:gosec // G101 -- it isn't a credential
	// OCMConfigKey defines the secret or configmap key to look for in case a user provides an ocm config.
	OCMConfigKey = ".ocmconfig"
	// OCMLabelDowngradable defines the secret.
	OCMLabelDowngradable = "ocm.software/ocm-k8s-toolkit/downgradable"
)

// Log levels.
const (
	// LevelDebug defines the depth at witch debug information is displayed.
	LevelDebug = 4
)

// Finalizers for the controllers.
const (
	// ResourceFinalizer makes sure that the resource is only deleted when it is no longer referenced by any other
	// deployer.
	ResourceFinalizer = "finalizers.ocm.software/resource"
	// ComponentFinalizer makes sure that the component is only deleted when it is no longer referenced by any other
	// resource.
	ComponentFinalizer = "finalizers.ocm.software/component"
	// RepositoryFinalizer makes sure that the OCM repository is only deleted when it is no longer referenced by any
	// other component.
	RepositoryFinalizer = "finalizers.ocm.software/repository"
)
