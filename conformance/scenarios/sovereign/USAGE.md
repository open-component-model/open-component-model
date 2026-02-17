# Sovereign Scenario Usage Guide

## Quick Start

Run the complete sovereign scenario with default settings:
```bash
task demo
```

## Configuration Options

### Image Registry Configuration

The scenario supports configurable image registry settings for flexibility in different environments:

| Variable | Default | Description |
|----------|---------|-------------|
| `IMAGE_REGISTRY` | `localhost:5001` | Registry URL for pushing/pulling images |
| `IMAGE_PREFIX` | `acme.org/sovereign` | Image name prefix/organization |
| `PUSH_IMAGE` | `true` | Whether to push images to registry |
| `VERSION` | `1.0.0` | Component version |
| `POSTGRES_VERSION` | `15` | PostgreSQL version to use |

### Usage Examples

#### Use Local Registry (Default)
```bash
# Uses localhost:5001 (started automatically)
task demo
```

#### Use External Registry
```bash
# Use Docker Hub
IMAGE_REGISTRY=docker.io IMAGE_PREFIX=myorg/sovereign task demo

# Use GitHub Container Registry
IMAGE_REGISTRY=ghcr.io IMAGE_PREFIX=myorg/sovereign task demo

# Use custom private registry
IMAGE_REGISTRY=registry.example.com:5000 IMAGE_PREFIX=team/sovereign task demo
```

#### Build Without Pushing
```bash
# Build images locally without pushing to registry
PUSH_IMAGE=false task build:app

# For testing/development
PUSH_IMAGE=false task test:build
```

#### Custom Versions
```bash
# Build with specific version
VERSION=2.0.0 task demo

# Use different PostgreSQL version
POSTGRES_VERSION=16 task demo
```

## Testing Phases

### Incremental Testing
Test each phase independently:
```bash
# 1. Check dependencies
task check

# 2. Start local registry if needed
task registry:start

# 3. Build and push images
task build:app

# 4. Build OCM components
task build:notes
task build:postgres
task build:product

# 5. Sign components
task sign

# 6. Verify signatures
task verify

# 7. Create cluster
task cluster:create

# 8. Deploy components
task cluster:deploy

# 9. Verify deployment
task verify:deployment
```

### Test Commands
```bash
# Run all tests incrementally
task test:incremental

# Test specific phases
task test:build    # Test building only
task test:sign     # Test signing/verification
task test:cluster  # Test cluster setup

# Debug tools
task validate:ctf      # Inspect CTF archive
task inspect:component # Show component descriptor
task logs             # View controller logs
task debug            # Debug failed deployment
```

## Cleanup

```bash
# Clean everything
task clean

# Clean specific resources
task clean:docker     # Remove Docker images
task registry:stop    # Stop local registry
kind delete cluster --name sovereign-conformance
```

## Common Scenarios

### Air-Gapped Deployment
```bash
# Build with local registry, then transfer to air-gap
task demo
```

### CI/CD Pipeline
```bash
# Use CI registry
IMAGE_REGISTRY=${CI_REGISTRY} \
IMAGE_PREFIX=${CI_PROJECT_PATH} \
VERSION=${CI_COMMIT_TAG} \
task demo
```

### Development Mode
```bash
# Fast iteration without push
PUSH_IMAGE=false task test:build
task validate:ctf
```

### Multi-Environment Testing
```bash
# Dev environment
IMAGE_REGISTRY=dev-registry.local:5000 VERSION=dev task demo

# Staging environment  
IMAGE_REGISTRY=stage-registry.local:5000 VERSION=stage task demo

# Production environment
IMAGE_REGISTRY=prod-registry.local:5000 VERSION=v1.0.0 task demo
```

## Troubleshooting

### Registry Connection Issues
```bash
# Test registry connectivity
docker pull ${IMAGE_REGISTRY}/hello-world:latest

# Check local registry
docker ps | grep local-registry
curl -X GET http://localhost:5001/v2/_catalog
```

### Build Failures
```bash
# Build with verbose output
task build:app --verbose

# Check Docker daemon
docker info

# Check disk space
df -h
```

### Component Issues
```bash
# Validate component descriptors
task inspect:component

# Check CTF archive contents
task validate:ctf
```

### Deployment Issues
```bash
# Check controller logs
task logs

# Debug deployment
task debug

# Check pod status
kubectl -n sovereign-product get pods -o wide
kubectl -n sovereign-product describe pods
```