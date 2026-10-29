# expect:
FROM --platform=$BUILDPLATFORM golang:1.25@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa AS build
FROM scratch
COPY --from=build /etc/passwd /etc/passwd
