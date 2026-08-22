# syntax=docker/dockerfile:1.6

# --- build stage ---
FROM golang:1.22-bookworm AS build

WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Source.
COPY . .

# Static, trimmed, reproducible.
ARG VERSION=v0.0.1-dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/reviewpilot ./cmd/reviewpilot

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/reviewpilot /usr/local/bin/reviewpilot

USER nonroot:nonroot
EXPOSE 8080

ENV RP_HTTP_LISTEN=":8080"

ENTRYPOINT ["/usr/local/bin/reviewpilot"]
