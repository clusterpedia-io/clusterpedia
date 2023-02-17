ARG BUILDER_IMAGE
FROM --platform=$BUILDPLATFORM ${BUILDER_IMAGE} as builder
WORKDIR /clusterpedia

ARG BIN_NAME
ARG TARGETARCH
RUN GOARCH=${TARGETARCH} /builder.sh ${BIN_NAME}

FROM alpine:3.17.2
RUN apk add --no-cache gcompat

ARG BIN_NAME
COPY --from=builder /clusterpedia/bin/${BIN_NAME} /usr/local/bin/${BIN_NAME}
