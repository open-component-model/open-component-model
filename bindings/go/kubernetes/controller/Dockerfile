# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

# renovate: datasource=github-releases depName=sigstore/cosign
ARG COSIGN_VERSION=v3.1.3

# Build the manager binary
FROM --platform=${BUILDPLATFORM} golang:1.27.1@sha256:162be5298a40ed317005c8339c6de4d10d3eef336d66dc8e9259b03ab9d3a6d2 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

# Copy the whole bindings/go module source. The controller is a package inside
# this module and imports sibling binding packages, so the build needs the full
# module tree, not just the controller subdirectory.
COPY . .

# Build
# GO_TAGS is empty by default; pass GO_TAGS=pprof to get a debug image that
# serves profiles on --pprof-bind-address.
ARG GO_TAGS=""
# Go Cryptographic Module version (FIPS 140-3). Required; the source of truth is
# GOFIPS140 in the project root .env, passed in by `task docker-build/multi-arch`.
ARG GOFIPS140
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call task docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
#
# The build and module caches are mounted with sharing=locked so that a
# multi-platform build serializes the per-arch `go build` invocations instead of
# running both at once. That keeps peak memory at roughly a single build and
# lets the second arch reuse the warmed caches. Dropping `-a` avoids force
# rebuilding the entire standard library on every build.
# GOTOOLCHAIN=local: build with the image's Go only; a toolchain mismatch with
# go.mod fails instead of downloading another toolchain.
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 GOTOOLCHAIN=local GOFIPS140="${GOFIPS140:?GOFIPS140 build arg is required, see root .env}" GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -tags "${GO_TAGS}" -o manager ./kubernetes/controller/cmd

# cosign is built from source because upstream release binaries are not linked
# against the Go Cryptographic Module. The sigstore signing handler in
# bindings/go/sigstore shells out to this binary; the version is pinned once in
# bindings/go/sigstore/signing/handler/internal/.env and passed in via
# --build-arg COSIGN_VERSION by the controller Taskfile.
FROM --platform=${BUILDPLATFORM} golang:1.27.1@sha256:e0174e51e81218523251d85d248a90d24c3d5e81543b4f07a5d66229397db190 AS cosign
ARG TARGETOS
ARG TARGETARCH
ARG COSIGN_VERSION
# Source of truth: root .env
ARG GOFIPS140
# go install refuses GOBIN for cross-compiles; the binary lands in /go/bin or
# /go/bin/${GOOS}_${GOARCH} depending on whether host and target match.
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 GOTOOLCHAIN=local \
    GOFIPS140="${GOFIPS140:?GOFIPS140 build arg is required, see root .env}" \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go install -trimpath -ldflags="-s -w" \
      "github.com/sigstore/cosign/v3/cmd/cosign@${COSIGN_VERSION:?COSIGN_VERSION build arg is required}" \
    && mkdir -p /out && find /go/bin -name cosign -type f -exec cp {} /out/cosign \;

# GnuPG comes from the Garden Linux FIPS image because its libgcrypt is the
# FIPS-capable build. The image itself is a full distro (apt, shell), so gpg and
# exactly its shared libraries are staged into /rootfs and copied into bare-libc.
# Keep this image and bare-libc (final stage) on the SAME Garden Linux release:
# the staged gpg libraries run under bare-libc's dynamic loader. Renovate updates
# both in one PR (group "Garden Linux images" in .github/renovate.json5).
FROM ghcr.io/gardenlinux/gardenlinux/fips:1877.25@sha256:74ae1f81d0723434024f591d8c2872852201b99b9f18c0fbe10221b16e60e01d AS gpg
# gpg-agent is needed even for verification: the handler creates a temporary
# GnuPG home and stops its agent with gpgconf --kill (gpg-connect-agent).
# dirmngr: gpgconf --show-versions exits non-zero without it.
RUN apt-get update \
    && apt-get install -y --no-install-recommends gpg gpg-agent gpgconf dirmngr \
    && rm -rf /var/lib/apt/lists/*
# Paths are canonicalised (Garden Linux is usrmerged: /lib -> usr/lib) so the
# staged tree never replaces the symlinks already present in bare-libc.
RUN set -eu; \
    bins="/usr/bin/gpg /usr/bin/gpg-agent /usr/bin/gpgconf /usr/bin/gpg-connect-agent /usr/bin/dirmngr"; \
    for f in $bins $(ldd $bins | awk '$2 == "=>" {print $3}' | sort -u); do \
      dst="/rootfs$(readlink -f "$(dirname "$f")")"; \
      mkdir -p "$dst" && cp -L "$f" "$dst/"; \
    done; \
    # libgcrypt enters FIPS mode when this file exists (its content is not
    # read); there is no kernel fips=1 to rely on inside a container.
    mkdir -p /rootfs/etc/gcrypt; \
    echo 1 > /rootfs/etc/gcrypt/fips_enabled; \
    # The handler writes temporary GnuPG homes under os.TempDir(); bare-libc has
    # no /tmp. Stage a world-writable one so the nonroot user can create them.
    mkdir -p -m 1777 /rootfs/tmp; \
    # CA bundle for OCI/registry TLS (bare-libc ships none; distroless did).
    mkdir -p /rootfs/etc/ssl/certs; \
    cp -L /etc/ssl/certs/ca-certificates.crt /rootfs/etc/ssl/certs/ca-certificates.crt

# Garden Linux bare-libc: shell-less FIPS userland base.
# https://docs.gardenlinux.org/how-to/container-base-image/bare.html
FROM ghcr.io/gardenlinux/gardenlinux/bare-libc:1877.25@sha256:ea38f985cd155f7a36389da0952cc0f95609d013d4bceffd7a522f3758b954f0
WORKDIR /
COPY --from=gpg /rootfs/ /
COPY --from=builder /workspace/manager .
# No GODEBUG needed: GOFIPS140 already makes fips140=on the default of both the
# manager and cosign (DefaultGODEBUG in their build info).
COPY --from=cosign /out/cosign /usr/local/bin/cosign
# cosign caches the Sigstore TUF trust root under $HOME/.sigstore. bare-libc
# sets no HOME, which resolves to / and is not writable by the nonroot user, so
# point it at the world-writable /tmp staged above. gpg is unaffected (explicit
# --homedir).
ENV HOME=/tmp
USER 65532:65532

ENTRYPOINT ["/manager"]
