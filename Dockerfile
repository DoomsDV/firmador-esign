# Multi-stage build for the multi-tenant SIFEN API (cmd/server).
FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/firmador ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=build /out/firmador /firmador

ENV SERVER_ADDR=:8080
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/firmador"]
