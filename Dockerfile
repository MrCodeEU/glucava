FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /glucava ./cmd/glucava

FROM debian:stable-slim
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
