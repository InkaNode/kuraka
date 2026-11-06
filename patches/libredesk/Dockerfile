# Builds Libredesk from a patched checkout (bash scripts/patches.sh checkout
# libredesk) with upstream's `make build`. The final stage copies upstream's
# Dockerfile so existing helpdesks run the same image layout.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS go

FROM --platform=$BUILDPLATFORM node:24-alpine AS build
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:/go/bin:$PATH GOPATH=/go CGO_ENABLED=0 COREPACK_ENABLE_DOWNLOAD_PROMPT=0
# GNU grep: the Makefile uses grep -P.
RUN apk add --no-cache make grep && corepack enable
WORKDIR /src
COPY . .
ARG LIBREDESK_VERSION TARGETOS TARGETARCH
# stuffbin runs here, so it is built for the build platform before
# GOOS/GOARCH switch the Libredesk binary to the target.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/knadh/stuffbin/... \
    && GOOS=$TARGETOS GOARCH=$TARGETARCH make build

FROM alpine:3.18
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /libredesk
COPY --from=build /src/libredesk .
COPY --from=build /src/config.sample.toml config.toml
EXPOSE 9000
CMD ["./libredesk"]
