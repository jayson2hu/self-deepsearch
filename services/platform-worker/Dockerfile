FROM golang:1.22-alpine AS build
WORKDIR /src
COPY services/platform-worker/go.mod services/platform-worker/go.sum* ./services/platform-worker/
WORKDIR /src/services/platform-worker
RUN go mod download
COPY services/platform-worker/ .
ARG BUILD_VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X self-deepsearch/services/platform-worker/internal/config.DefaultBuildVersion=${BUILD_VERSION}" \
    -o /out/platform-worker ./cmd/worker \
    && mkdir -p /out/history-archives

FROM gcr.io/distroless/static-debian12:nonroot
ARG BUILD_VERSION=dev
ARG BUILD_REVISION=unknown
LABEL org.opencontainers.image.title="self-deepsearch-platform-worker" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.revision="${BUILD_REVISION}"
COPY --from=build /out/platform-worker /platform-worker
COPY --from=build --chown=nonroot:nonroot /out/history-archives /var/lib/self-deepsearch/history-archives
EXPOSE 8081
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/platform-worker", "--healthcheck"]
ENTRYPOINT ["/platform-worker"]
