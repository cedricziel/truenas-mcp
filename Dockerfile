# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

# Dependencies first, so source edits do not invalidate the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/truenas-mcp ./cmd/truenas-mcp

# distroless static carries CA certificates, which the server needs to verify
# the target's TLS, and runs as an unprivileged user with no shell.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/truenas-mcp /usr/local/bin/truenas-mcp

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/truenas-mcp"]
