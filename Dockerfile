ARG BUILDER_IMAGE
FROM --platform=$BUILDPLATFORM ${BUILDER_IMAGE} as builder
WORKDIR /clusterpedia

ARG BIN_NAME
ARG TARGETARCH
RUN GOARCH=${TARGETARCH} /builder.sh ${BIN_NAME}

FROM alpine:3.18.5
RUN apk add --no-cache gcompat

# https://pkg.go.dev/net#hdr-Name_Resolution
ENV GODEBUG=netdns=go

ARG BIN_NAME
COPY --from=builder /clusterpedia/bin/${BIN_NAME} /usr/local/bin/${BIN_NAME}
