FROM oven/bun:1.3.14-slim AS base
WORKDIR /app

FROM base AS dependencies
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --registry=https://registry.npmjs.org

FROM base AS build
COPY --from=dependencies /app/node_modules ./node_modules
COPY package.json fibel.config.ts ./
COPY docs ./docs
COPY assets ./assets
RUN bun run docs:build

FROM base AS runtime
ARG VERSION=dev
ENV NODE_ENV=production
ENV PORT=3000
LABEL org.opencontainers.image.version="${VERSION}"

COPY --from=dependencies --chown=bun:bun /app/node_modules ./node_modules
COPY --from=build --chown=bun:bun /app/package.json ./package.json
COPY --from=build --chown=bun:bun /app/fibel.config.ts ./fibel.config.ts
COPY --from=build --chown=bun:bun /app/docs ./docs
COPY --from=build --chown=bun:bun /app/assets ./assets
COPY --from=build --chown=bun:bun /app/.fibel ./.fibel
COPY --from=build --chown=bun:bun /app/dist ./dist
COPY --chown=bun:bun scripts/serve-docs.ts ./scripts/serve-docs.ts

USER bun
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["bun", "-e", "const port = process.env.PORT ?? '3000'; const response = await fetch('http://127.0.0.1:' + port + '/en'); if (!response.ok) process.exit(1)"]
CMD ["bun", "run", "docs:start"]
