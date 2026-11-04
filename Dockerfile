FROM golang:1.27-alpine AS build

ENV CGO_ENABLED=0
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/kuraka ./cmd/kuraka \
    && mkdir -p /out/data \
    && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source="https://github.com/InkaNode/kuraka" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.title="Kuraka"

COPY --from=build /out/kuraka /usr/local/bin/kuraka
COPY --from=build --chown=65532:65532 /out/data /data

ENV KURAKA_DB=/data/kuraka.db KURAKA_LISTEN=:8080
USER 65532:65532
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/kuraka","healthcheck"]
ENTRYPOINT ["/usr/local/bin/kuraka"]
