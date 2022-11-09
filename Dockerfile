ARG BUILDER_IMAGE
ARG BASE_IMAGE

FROM --platform=$BUILDPLATFORM ${BUILDER_IMAGE} as builder
WORKDIR /clusterpedia

ARG BIN_NAME
ARG TARGETARCH
RUN GOARCH=${TARGETARCH} /builder.sh ${BIN_NAME}

FROM ${BASE_IMAGE}
RUN apk add --no-cache gcompat

ARG BIN_NAME
COPY --from=builder /clusterpedia/bin/${BIN_NAME} /usr/local/bin/${BIN_NAME}
