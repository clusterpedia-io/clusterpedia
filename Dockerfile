# To avoid the `InvalidDefaultArgInFrom` warning during build time,
# we add a non-existent default builder image.
# https://docs.docker.com/reference/build-checks/invalid-default-arg-in-from/
ARG BUILDER_IMAGE='clusterpedia.io/builder:not-build'
FROM --platform=$BUILDPLATFORM ${BUILDER_IMAGE} AS builder
WORKDIR /clusterpedia

ARG BIN_NAME
ARG EXTRA_BIN_NAMES=""
ARG TARGETARCH
# The builder image may already contain /clusterpedia/bin, so the binaries are output to /out/bin.
RUN set -e; for bin in ${BIN_NAME} ${EXTRA_BIN_NAMES}; do OUTPUT_DIR=/out GOARCH=${TARGETARCH} /builder.sh ${bin}; done

# https://alpinelinux.org/releases/
# Once we select a branch, we will continue to use the relevant version until it ends support.
# The new branch selection must ensure that the patch version >= 3.
FROM alpine:3.24.4
RUN apk add --no-cache gcompat

# https://pkg.go.dev/net#hdr-Name_Resolution
ENV GODEBUG=netdns=go

COPY --from=builder /out/bin/ /usr/local/bin/
