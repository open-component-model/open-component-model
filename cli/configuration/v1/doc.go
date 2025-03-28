// Package v1 supports finding, loading and parsing OCM configuration files.
//
// The file format is YAML and follows the same type mechanism used for all
// typed specifications in the OCM ecosystem. The file must have the type of
// ocm configuration specification. Alternatively, the client supports a generic
// configuration format capable of hosting a list of arbitrary configuration
// specifications. The type for this specification is «generic.config.ocm.software/v1»
// for example:
//
//	   type: generic.config.ocm.software/v1
//	   configurations:
//		 - type: credentials.config.ocm.software
//		   consumers:
//			 - identity:
//				 type: OCIRegistry
//				 hostname: ghcr.io
//				 pathprefix: open-component-model
//			   credentials:
//				 - type: Credentials
//				   properties:
//					 username: open-component-model
//					 password: some-token
//		   repositories:
//			 - repository:
//				 type: DockerConfig/v1
//				 dockerConfigFile: "~/.docker/config.json"
//				 propagateConsumerIdentity: true
package v1
