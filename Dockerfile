FROM node:26-alpine AS frontend-build
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:alpine AS backend-build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/
COPY --from=frontend-build /app/frontend/dist ./pkg/frontend/dist
RUN CGO_ENABLED=0 go build -o /otel-magnify ./cmd/server/

FROM alpine:3.23
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 -S magnify \
    && adduser -u 10001 -S magnify -G magnify \
    && mkdir -p /data \
    && chown magnify:magnify /data
COPY --from=backend-build /otel-magnify /usr/local/bin/otel-magnify
USER magnify:magnify
WORKDIR /data
VOLUME ["/data"]
EXPOSE 8080 4320
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["otel-magnify"]
