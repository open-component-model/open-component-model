# Multi-stage build for sovereign-notes application
FROM --platform=${BUILDPLATFORM} golang:1.26-alpine@sha256:91eda9776261207ea25fd06b5b7fed8d397dd2c0a283e77f2ab6e91bfa71079d AS builder
ARG TARGETOS
ARG TARGETARCH

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with version injection.
# v1.0.0 builds from cmd/sovereign-notes-v1 (initial schema, no migrations).
# v1.1.0+ builds from cmd/sovereign-notes (migration-based schema evolution).
ARG VERSION=1.0.0
RUN CMD_DIR=$(if [ "${VERSION}" = "1.0.0" ]; then echo "sovereign-notes-v1"; else echo "sovereign-notes"; fi) && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -installsuffix cgo \
    -ldflags="-X main.version=${VERSION}" \
    -o sovereign-notes ./cmd/${CMD_DIR}

# Final stage
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

# Install CA certificates and create non-root user
RUN apk --no-cache add ca-certificates \
    && addgroup -g 1001 -S notes \
    && adduser -u 1001 -S notes -G notes

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/sovereign-notes .

# Change ownership to notes user
RUN chown notes:notes /app/sovereign-notes

# Switch to non-root user
USER notes

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/readyz || exit 1

# Run the application
CMD ["./sovereign-notes"]