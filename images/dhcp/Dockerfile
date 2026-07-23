# syntax=docker/dockerfile:1

FROM debian:bookworm-slim AS build

ARG DNSMASQ_VERSION=2.93

RUN apt-get update \
    && apt-get install --yes --no-install-recommends \
        build-essential \
        ca-certificates \
        curl \
        file \
        linux-libc-dev \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

RUN curl --fail --location --silent --show-error \
        "https://thekelleys.org.uk/dnsmasq/dnsmasq-${DNSMASQ_VERSION}.tar.xz" \
        | tar --extract --xz --strip-components=1

RUN sed --in-place 's/^#define CHUSER "nobody"$/#define CHUSER 0/' src/config.h \
    && make --jobs="$(nproc)" CC=gcc LDFLAGS=-static \
    && strip src/dnsmasq \
    && file src/dnsmasq | grep --quiet "statically linked"

FROM scratch

COPY --from=build /src/src/dnsmasq /dnsmasq

ENTRYPOINT ["/dnsmasq"]
