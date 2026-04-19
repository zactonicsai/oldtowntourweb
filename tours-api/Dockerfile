# syntax=docker/dockerfile:1.7

# ---- build stage ----
# Pinned to alpine3.23 so the base OS version is explicit. This
# resolves to Go 1.26.2 on Alpine 3.23.3 as of April 2026; bump
# when a new minor arrives.
FROM golang:1.26-alpine3.23 AS build

WORKDIR /src

# Cache the module download layer by copying go.mod first.
COPY go.mod ./
# go.sum exists only after `go mod tidy` adds deps — copy if present.
COPY go.su[m] ./
RUN go mod download

COPY . .

# Static binary so it can run from scratch/distroless.
# -trimpath strips local paths for reproducibility.
# -ldflags "-s -w" drops debug + symbol tables to shrink the image.
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/tours-api \
    ./cmd/server

# Create an empty /data in the builder so we can copy it across with
# the right ownership. Distroless has no shell, so we can't mkdir there.
RUN mkdir -p /out/data

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Static assets the server will serve if WEB_ROOT resolves to ./web.
# (Unused when running behind nginx via docker compose — nginx bind-mounts
# ./web itself — but kept here so the image works standalone.)
COPY --from=build /src/web /app/web
COPY --from=build /out/tours-api /app/tours-api

# /data owned by nonroot (uid 65532). Named volumes inherit ownership
# from this path on first mount, so this is what lets the nonroot
# binary actually write to the volume.
COPY --from=build --chown=65532:65532 /out/data /data
ENV DATA_DIR=/data
VOLUME ["/data"]

EXPOSE 8080

# nonroot user is uid 65532 in the distroless image.
USER nonroot:nonroot

ENTRYPOINT ["/app/tours-api"]
