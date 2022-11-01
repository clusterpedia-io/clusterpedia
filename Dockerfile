ARG BUILDER_IMAGE
ARG BASE_IMAGE

FROM --platform=$BUILDPLATFORM ${BUILDER_IMAGE} as builder
WORKDIR /clusterpedia

ARG BIN_NAME
ARG ON_PLUGINS
ARG TARGETARCH
RUN GOARCH=${TARGETARCH} ON_PLUGINS=${ON_PLUGINS} make ${BIN_NAME}

FROM ${BASE_IMAGE}

ARG ON_PLUGINS
RUN if [ "${ON_PLUGINS}" = "true" ]; then apk add --no-cache gcompat; fi

ARG BIN_NAME
COPY --from=builder /clusterpedia/bin/${BIN_NAME} /usr/local/bin/${BIN_NAME}
