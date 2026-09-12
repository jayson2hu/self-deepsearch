FROM golang:1.22-alpine AS build
WORKDIR /src
COPY services/platform-api/go.mod services/platform-api/go.sum* ./services/platform-api/
WORKDIR /src/services/platform-api
RUN go mod download
COPY services/platform-api/ .
ARG BUILD_VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X self-deepsearch/services/platform-api/internal/config.DefaultBuildVersion=${BUILD_VERSION}" \
    -o /out/platform-api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w" \
    -o /out/create-owner ./cmd/create-owner

FROM gcr.io/distroless/static-debian12:nonroot
ARG BUILD_VERSION=dev
ARG BUILD_REVISION=unknown
LABEL org.opencontainers.image.title="self-deepsearch-platform-api" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.revision="${BUILD_REVISION}"
COPY --from=build /out/platform-api /platform-api
COPY --from=build /out/create-owner /create-owner
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/platform-api", "--healthcheck"]
ENTRYPOINT ["/platform-api"]
