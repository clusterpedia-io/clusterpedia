FROM golang:1.19.4

# FROM golang:1.19.2-alpine
# RUN apk add --no-cache build-base git bash

RUN apt-get update && apt-get install -y gcc-aarch64-linux-gnu gcc-x86-64-linux-gnu

COPY . /clusterpedia

# RUN rm -rf /clusterpedia/.git
# RUN rm -rf /clusterpedia/test

ENV CLUSTERPEDIA_REPO="/clusterpedia"

RUN cp /clusterpedia/hack/builder.sh /
