FROM python:3.12-slim
RUN useradd --create-home --uid 10001 collector
WORKDIR /app
COPY workers/collector-python/ /app/
USER collector
ENTRYPOINT ["python", "-m", "collector.cli"]

