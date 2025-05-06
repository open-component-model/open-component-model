// Package repository provides functionality for creating and managing OCI repositories
// in the Open Component Model (OCM) context based on serializable specifications.
// It supports both CTF (Common Transport Format) and OCI registry-based repositories.
//
// The package provides two main repository types:
//   - CTF Repository: A file-based repository that stores components in a CTF archive
//   - OCI Repository: A registry-based repository that stores components in an OCI registry
//
// Key features:
//   - Creation of repositories from specifications
//   - Support for different access modes (read-only, read-write, create)
//   - Integration with OCI registries and CTF archives
//   - Component version management
//   - Resource handling and access
//
// Example usage:
//
//	// Create a CTF repository
//	ctfRepo := &ctfrepospecv1.Repository{
//		Path:       "/path/to/ctf",
//		AccessMode: ctfrepospecv1.AccessModeReadWrite,
//	}
//	repo, err := NewFromCTFRepoV1(ctx, ctfRepo)
//
//	// Create an OCI repository
//	ociRepo := &ocirepospecv1.Repository{
//		BaseUrl: "https://registry.example.com",
//	}
//	client := remote.NewRepository("registry.example.com").Client
//	repo, err := NewFromOCIRepoV1(ctx, ociRepo, client)
package repository
