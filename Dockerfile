FROM golang:1.27@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /glucava ./cmd/glucava

FROM debian:stable-slim@sha256:5bc3287b25407c965a30f38e32603dc253a3869e1b12a21ac09bfc27fd8b13ce
RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium ca-certificates fonts-noto-color-emoji \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 10001 glucava \
 && mkdir /data && chown glucava /data
COPY --from=build /glucava /usr/local/bin/glucava
USER glucava
ENV CHROME_PATH=/usr/bin/chromium GLUCAVA_NO_SANDBOX=1
VOLUME /data
EXPOSE 8090
ENTRYPOINT ["glucava"]
CMD ["serve", "--dir", "/data", "--http", "0.0.0.0:8090"]
