# ---------- build ----------
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w" \
      -o /out/worker ./cmd/worker

# ---------- runtime ----------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata iproute2 && \
    adduser -D -u 10001 worker

WORKDIR /app

# Binary
COPY --from=builder /out/worker /app/worker

# Baked-in read-only seed data
COPY templates/ /app/templates/
COPY inputs/    /app/inputs/

# Writable runtime dirs (assets are generated at runtime)
RUN mkdir -p /app/assets && \
    chown -R worker:worker /app/assets /app/inputs

USER worker

EXPOSE 8033/udp 8033/tcp 69/udp 67/udp

ENTRYPOINT ["/app/worker"]
