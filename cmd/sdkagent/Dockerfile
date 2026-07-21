FROM golang:1.25.12@sha256:9006890ecba0a168034d99516084099ae3114d9f2b7d6572c77f2dde57ebc980 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/sdkagent ./cmd/sdkagent

# :nonroot ships uid/gid 65532 — no shell, no package manager.
# HEALTHCHECK is omitted: sdkagent is a WebSocket OpAMP client with no
# listener, and distroless has no wget/curl to probe one anyway.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b
COPY --from=build /out/sdkagent /usr/local/bin/sdkagent
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/sdkagent"]
