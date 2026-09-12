FROM node:22-alpine AS dependencies
WORKDIR /app
COPY package.json package-lock.json ./
COPY apps/display-web/package.json ./apps/display-web/package.json
COPY apps/ops-web/package.json ./apps/ops-web/package.json
COPY packages/api-contracts/package.json ./packages/api-contracts/package.json
RUN npm ci

FROM dependencies AS build
COPY tsconfig.base.json ./
COPY packages/api-contracts/ ./packages/api-contracts/
COPY apps/ops-web/ ./apps/ops-web/
RUN npm run build --workspace @self-deepsearch/ops-web

FROM node:22-alpine AS runtime
ARG BUILD_VERSION=dev
ARG BUILD_REVISION=unknown
LABEL org.opencontainers.image.title="self-deepsearch-ops-web" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.revision="${BUILD_REVISION}"
ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3001
WORKDIR /app
COPY --from=build /app/apps/ops-web/.next/standalone ./
COPY --from=build /app/apps/ops-web/.next/static ./apps/ops-web/.next/static
RUN install -d -o node -g node /app/apps/ops-web/.next/cache
EXPOSE 3001
USER node
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD ["wget", "--quiet", "--spider", "http://127.0.0.1:3001/login"]
CMD ["node", "apps/ops-web/server.js"]
