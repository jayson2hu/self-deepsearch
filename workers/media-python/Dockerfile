FROM python:3.12-slim
ARG BUILD_VERSION=dev
ARG BUILD_REVISION=unknown
LABEL org.opencontainers.image.title="self-deepsearch-media-python" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.revision="${BUILD_REVISION}"
RUN useradd --create-home --uid 10001 media \
    && install -d -o media -g media -m 0700 /var/lib/self-deepsearch/media /var/lib/self-deepsearch/media-state
WORKDIR /app
COPY workers/media-python/ /app/
ARG PIP_INDEX_URL=https://pypi.org/simple
RUN pip install --no-cache-dir .
USER media
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["python", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8090/health/live', timeout=2).read()"]
ENTRYPOINT ["python", "-m", "media_worker.cli"]
