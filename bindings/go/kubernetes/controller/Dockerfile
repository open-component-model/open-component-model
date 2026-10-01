# syntax=docker/dockerfile:1@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32
# Build the manager binary
FROM --platform=${BUILDPLATFORM} golang:1.27.1@sha256:e0174e51e81218523251d85d248a90d24c3d5e81543b4f07a5d66229397db190 AS builder
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
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 GOFIPS140=v1.26.0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -tags "${GO_TAGS}" -o manager ./kubernetes/controller/cmd

# Garden Linux FIPS base image; the binary embeds the frozen Go FIPS 140-3 module (GOFIPS140).
FROM --platform=${TARGETPLATFORM} ghcr.io/gardenlinux/gardenlinux/fips:1877.25@sha256:74ae1f81d0723434024f591d8c2872852201b99b9f18c0fbe10221b16e60e01d AS base

# DISA container hardening: no executable may grant higher privileges, so strip
# setuid/setgid bits from the base root filesystem. This runs on the build
# platform against a copy of the target rootfs, so no emulation is needed for
# cross-architecture builds.
FROM --platform=${BUILDPLATFORM} ghcr.io/gardenlinux/gardenlinux/fips:1877.25@sha256:74ae1f81d0723434024f591d8c2872852201b99b9f18c0fbe10221b16e60e01d AS harden
COPY --from=base / /rootfs
RUN find /rootfs -xdev -perm /6000 -type f -exec chmod ug-s {} +

FROM scratch
COPY --from=harden /rootfs /
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
