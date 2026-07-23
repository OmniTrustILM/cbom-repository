########################
# Build Stage
########################
FROM golang:1.26-alpine3.22 AS builder

ARG VERSION=dev
ENV CGO_ENABLED=0 \
    GOFLAGS="-trimpath" \
    LDFLAGS="-s -w -X main.version=${VERSION}"

WORKDIR /src

# Better layer caching for deps
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Copy the rest and build
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${LDFLAGS}" -o /out/cbom-repository ./cmd/cbom-repository

########################
# Run Stage
########################
FROM alpine:3.24.1

# apk upgrade should be removed once CVE-2026-22184 will be fixed
RUN apk update && apk upgrade --no-cache

LABEL org.opencontainers.image.authors="OmniTrustILM <support@omnitrust.com>"

# add non root user omnitrust
RUN addgroup --system --gid 10001 omnitrust && adduser --system --home /opt/omnitrust --uid 10001 --ingroup omnitrust omnitrust

COPY --from=builder /out/cbom-repository /usr/local/bin/cbom-repository

ENV APP_LOG_LEVEL=INFO

EXPOSE 8080

USER 10001

ENTRYPOINT ["/usr/local/bin/cbom-repository"]
