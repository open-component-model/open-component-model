# Makefile wrapper for kubebuilder helm/v2-alpha plugin compatibility
# This delegates to Taskfile for actual implementation
# Note: These targets avoid cycles by NOT depending on the full manifests task
generate:
	@task generate
manifests:
	@task manifests
build-installer:
	@task build-installer
