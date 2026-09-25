FROM golang:1.27@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.buildID=${VERSION}" -o /glucava ./cmd/glucava

FROM debian:stable-slim@sha256:5bc3287b25407c965a30f38e32603dc253a3869e1b12a21ac09bfc27fd8b13ce
RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium ca-certificates fonts-noto-color-emoji \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 10001 glucava \
 && mkdir /data && chown glucava /data
COPY --from=build /glucava /usr/local/bin/glucava
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
ENTRYPOINT ["glucava"]
CMD ["serve", "--dir", "/data", "--http", "0.0.0.0:8090"]
