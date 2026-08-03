# Multi-stage build for the multi-tenant SIFEN API (cmd/server).
FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Build nativo del host (el VPS OCI es aarch64; en amd64 produce amd64).
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/firmador ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=build /out/firmador /firmador

ENV SERVER_ADDR=:8080
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/firmador"]
