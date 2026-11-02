# syntax=docker/dockerfile:1.7

FROM node:24-alpine AS frontend

WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend ./
RUN npm run build

FROM golang:1.27-alpine3.23 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY domain ./domain
COPY application ./application
COPY infrastructure ./infrastructure
COPY interface ./interface
COPY factory ./factory
COPY bootstrap ./bootstrap
COPY main.go ./main.go
COPY --from=frontend /app/interface/web/dist ./interface/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/retroxide .

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
    RETROXIDE_CONFIG_FILE=/app/data/retroxide.env \
    RETROXIDE_SQLITE_PATH=/app/data/retroxide.sqlite3 \
    RETROXIDE_STORAGE_ROOT=/app/storage \
    RETROXIDE_WORKER_COUNT=1

CMD ["retroxide"]
