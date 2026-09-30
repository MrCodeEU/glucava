# --platform=$BUILDPLATFORM pins this stage to the build host's native arch
# (amd64, on GitHub's runners) regardless of which platform buildx is
# assembling: Go cross-compiles natively via GOOS/GOARCH, so the arm64
# binary is built without QEMU emulating the whole compile, which used to
# make the multi-platform image build take ~15 minutes.
FROM --platform=$BUILDPLATFORM golang:1.27@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w -X main.buildID=${VERSION}" -o /glucava ./cmd/glucava

# Typst typesets the PDF report (internal/report). A static musl build is
# downloaded for the target architecture and checked against a pinned sha256;
# bumping the version means updating all three ARGs (the hashes are of the
# release tarballs on github.com/typst/typst/releases).
FROM --platform=$BUILDPLATFORM debian:stable-slim@sha256:5bc3287b25407c965a30f38e32603dc253a3869e1b12a21ac09bfc27fd8b13ce AS typst
ARG TYPST_VERSION=0.15.1
ARG TYPST_SHA256_AMD64=a6d077d0a95eed5a2eba715b2dae06be954f624ccbf85758a03f389ded33118c
ARG TYPST_SHA256_ARM64=5aa8d74a3d906e60ea12a66ac2f37f8eef1b14cbad7182a745e393a10c23dcee
ARG TARGETARCH
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl xz-utils \
 && rm -rf /var/lib/apt/lists/* \
 && case "${TARGETARCH}" in \
      amd64) triple=x86_64-unknown-linux-musl; sha="${TYPST_SHA256_AMD64}" ;; \
      arm64) triple=aarch64-unknown-linux-musl; sha="${TYPST_SHA256_ARM64}" ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && curl -fsSL -o /tmp/typst.tar.xz "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-${triple}.tar.xz" \
 && echo "${sha}  /tmp/typst.tar.xz" | sha256sum -c - \
 && tar -xJf /tmp/typst.tar.xz -C /tmp "typst-${triple}/typst" \
 && install -m 0755 "/tmp/typst-${triple}/typst" /typst \
 && rm -rf /tmp/typst*

FROM debian:stable-slim@sha256:5bc3287b25407c965a30f38e32603dc253a3869e1b12a21ac09bfc27fd8b13ce
RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium ca-certificates fonts-noto-color-emoji tini \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 10001 glucava \
 && mkdir /data && chown glucava /data
COPY --from=build /glucava /usr/local/bin/glucava
COPY --from=typst /typst /usr/local/bin/typst
USER glucava
ARG VERSION=dev
LABEL org.opencontainers.image.title="glucava" \
      org.opencontainers.image.description="Adds Dexcom glucose stats to your Strava activities" \
      org.opencontainers.image.source="https://github.com/MrCodeEU/glucava" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version="${VERSION}"
ENV CHROME_PATH=/usr/bin/chromium GLUCAVA_NO_SANDBOX=1
VOLUME /data
EXPOSE 8090
# The slim image has no curl, so the binary checks itself.
HEALTHCHECK --interval=30s --timeout=6s --start-period=30s --retries=3 CMD ["glucava", "healthcheck"]
# tini reaps the processes Chrome leaves behind; as PID 1 glucava would not, and
# they pile up as zombies until the container hits its pids limit.
ENTRYPOINT ["tini", "--", "glucava"]
CMD ["serve", "--dir", "/data", "--http", "0.0.0.0:8090"]
