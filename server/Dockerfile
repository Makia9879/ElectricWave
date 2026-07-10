# syntax=docker/dockerfile:1

# ---- build stage ----
# Run the Go toolchain on the NATIVE builder arch ($BUILDPLATFORM) and cross-compile
# to linux/amd64. This avoids running `go build` under QEMU emulation, which is slow
# and can crash on arm64 hosts. Go cross-compiles natively with no CGO.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/notice ./cmd/notice

# ---- runtime stage ----
FROM alpine:3.20
# ca-certificates for any outbound TLS (future providers); wget for HEALTHCHECK.
RUN apk add --no-cache ca-certificates wget && \
    addgroup -S app && adduser -S app -G app && \
    mkdir -p /data && chown -R app:app /data

COPY --from=build /out/notice /usr/local/bin/notice

# Persist SQLite data under /data. Override with STORAGE_PATH if needed.
ENV STORAGE_PATH=/data/notice.db \
    HTTP_ADDR=:8788

USER app
WORKDIR /home/app

EXPOSE 8788

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8788/healthz || exit 1

ENTRYPOINT ["notice"]
