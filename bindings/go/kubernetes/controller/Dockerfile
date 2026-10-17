# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

# Build the manager binary
FROM --platform=${BUILDPLATFORM} golang:1.27.2@sha256:820153096fda4de6ac1b7194d12e416b7dd8f4e9a33d17e0345b111b945162d8 AS builder
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

# Static cosign linked against the Go Cryptographic Module (GOFIPS140=certified,
# fips140=on by default), built and published from
# https://github.com/open-component-model/cosign. Same release as COSIGN_VERSION in
# bindings/go/sigstore/signing/handler/internal/.env (Renovate group "OCM cosign";
# pin_sync_test.go there fails if they diverge). The sigstore signing handler in
# bindings/go/sigstore shells out to this binary.
FROM ghcr.io/open-component-model/cosign:v3.1.3-ocm.27@sha256:ca2b9b32ab93f51c61db25c651ef8d13e7c60553fecf667c4f758e19e69db555 AS cosign

# Stage filesystem additions for the scratch final image. Only files are copied,
# so the stage runs on the build platform.
FROM --platform=${BUILDPLATFORM} ghcr.io/gardenlinux/gardenlinux/fips:1877.25@sha256:74ae1f81d0723434024f591d8c2872852201b99b9f18c0fbe10221b16e60e01d AS certs
RUN set -eu; \
    # The manager and cosign write temporary files under os.TempDir(); scratch
    # has no /tmp. Stage a world-writable one so the nonroot user can create them.
    mkdir -p -m 1777 /rootfs/tmp; \
    # CA bundle for OCI/registry TLS (Garden Linux FIPS userland).
    mkdir -p /rootfs/etc/ssl/certs; \
    cp -L /etc/ssl/certs/ca-certificates.crt /rootfs/etc/ssl/certs/ca-certificates.crt

# manager and cosign are static (CGO_ENABLED=0), so the image needs no libc.
FROM scratch
# scratch defines no PATH; the sigstore signing handler looks up cosign on PATH.
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
WORKDIR /
COPY --from=certs /rootfs/ /
COPY --from=builder /workspace/manager .
# No GODEBUG needed: GOFIPS140 already makes fips140=on the default of both the
# manager and cosign (DefaultGODEBUG in their build info).
COPY --from=cosign /cosign /usr/local/bin/cosign
# cosign caches the Sigstore TUF trust root under $HOME/.sigstore. scratch sets
# no HOME, which resolves to / and is not writable by the nonroot user, so point
# it at the world-writable /tmp staged above.
ENV HOME=/tmp
USER 65532:65532

ENTRYPOINT ["/manager"]
