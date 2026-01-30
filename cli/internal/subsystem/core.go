package subsystem

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

func init() {
	Register(ocmRepositorySystem)
	Register(ocmRepositoryListerSystem)
	Register(ocmResourceRepositorySystem)
	Register(inputSystem)
	Register(credentialRepositorySystem)
	Register(signingHandlerSystem)
}

var (
	ocmRepositorySystem = NewSubsystem(
		"ocm-repository",
		"Repositories for storing and managing OCM component versions.",
	)
	ocmRepositoryListerSystem = NewSubsystem(
		"ocm-repository-lister",
		"Listers for listing OCM component repositories. Can be seen as repository of versioned repositories",
	)
	ocmResourceRepositorySystem = NewSubsystem(
		"ocm-resource-repository",
		"Repositories for storing and managing OCM resources.",
	)
	inputSystem = NewSubsystem(
		"input",
		"Input methods define how content is sourced and ingested into an OCM component version.",
	)
	credentialRepositorySystem = NewSubsystem(
		"credential-repository",
		"Repositories for storing and managing credentials so they can be referenced in the OCM credential graph.",
	)
	signingHandlerSystem = NewSubsystem(
		"signing",
		"Signing handlers are responsible for signing and verification of component versions.",
	)
	// TODO: blob transformer registry does not yet expose a scheme
	// blobTransformerSystem = NewSubsystem(
	// 	"blob-transformer",
	// 	"Blob transformers are responsible for transforming blobs into other formats. They are mainly used for input digestion
	// 	when adding new resources into a component version",
	// )

	// TODO: transformer registry does not yet expose a scheme
	// graphTransformer = NewSubsystem(
	// 	"graph-transformer",
	// 	"Transformation Graph Transformers are responsible for acting as atomic transforming units inside a transformation graph.
	//	Transformation Graphs are used in complex operations such as transferring of component versions, but can also delegate
	//  to other plugins and transformer types.",
	// )
)

func RegisterPluginManager(pm *manager.PluginManager) error {
	return errors.Join(
		ocmRepositorySystem.Scheme.RegisterScheme(pm.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryScheme()),
		ocmRepositoryListerSystem.Scheme.RegisterScheme(pm.ComponentListerRegistry.GetComponentVersionRepositoryScheme()),
		ocmResourceRepositorySystem.Scheme.RegisterScheme(pm.ResourcePluginRegistry.ResourceScheme()),
		inputSystem.Scheme.RegisterScheme(pm.InputRegistry.InputRepositoryScheme()),
		credentialRepositorySystem.Scheme.RegisterScheme(pm.CredentialRepositoryRegistry.RepositoryScheme()),
		signingHandlerSystem.Scheme.RegisterScheme(pm.SigningRegistry.ResourceScheme()),
		// blobTransformerSystem.Scheme.RegisterScheme(pm.BlobTransformerRegistry.TransformerScheme())
		// graphTransformer.Scheme.RegisterScheme(pm.GraphTransformerRegistry.TransformerScheme())
	)
}
