# To avoid the `InvalidDefaultArgInFrom` warning during build time,
# we add a non-existent default builder image.
# https://docs.docker.com/reference/build-checks/invalid-default-arg-in-from/
ARG BUILDER_IMAGE='clusterpedia.io/builder:not-build'
FROM --platform=$BUILDPLATFORM ${BUILDER_IMAGE} AS builder
WORKDIR /clusterpedia

ARG BIN_NAME
ARG TARGETARCH
RUN GOARCH=${TARGETARCH} /builder.sh ${BIN_NAME}

# https://alpinelinux.org/releases/
# Once we select a branch, we will continue to use the relevant version until it ends support.
# The new branch selection must ensure that the patch version >= 3.
FROM alpine:3.21.4
RUN apk add --no-cache gcompat

# https://pkg.go.dev/net#hdr-Name_Resolution
ENV GODEBUG=netdns=go

ARG BIN_NAME
COPY --from=builder /clusterpedia/bin/${BIN_NAME} /usr/local/bin/${BIN_NAME}
