# syntax=docker/dockerfile:1.7

FROM golang:1.27-alpine3.23 AS builder

WORKDIR /app
RUN apk add --no-cache build-base

COPY go.mod go.sum ./
RUN go mod download

COPY internal ./internal
COPY main.go ./main.go
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/retroxide .

FROM alpine:3.23 AS runtime

WORKDIR /app
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 -S retroxide \
    && adduser -u 10001 -S -D -G retroxide retroxide \
    && mkdir -p /app/storage /app/data \
    && touch /app/data/retroxide.sqlite3 \
    && chown -R retroxide:retroxide /app

COPY --from=builder /out/retroxide /usr/local/bin/retroxide

USER retroxide
EXPOSE 3000

ENV GIN_MODE=release \
    RETROXIDE_ENV=prod \
    RETROXIDE_HOST=0.0.0.0 \
    RETROXIDE_PORT=3000 \
    RETROXIDE_DB_DRIVER=sqlite \
    RETROXIDE_SQLITE_PATH=/app/data/retroxide.sqlite3 \
    RETROXIDE_STORAGE_ROOT=/app/storage \
    RETROXIDE_WORKER_COUNT=1

CMD ["retroxide"]
