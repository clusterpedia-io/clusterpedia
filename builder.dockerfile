FROM golang:1.23.12
RUN apt-get update && apt-get install -y gcc-aarch64-linux-gnu gcc-x86-64-linux-gnu

COPY . /clusterpedia
ENV CLUSTERPEDIA_REPO="/clusterpedia"
RUN cp /clusterpedia/hack/builder.sh /
