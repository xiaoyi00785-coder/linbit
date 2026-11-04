# syntax=docker/dockerfile:1
#
# Web frontend (apps/web, Next.js) image for the pnpm monorepo.
# Build from the repo root:
#   docker build -f deploy/next.Dockerfile \
#     --build-arg NEXT_PUBLIC_SITE_URL=https://www.cowa.click \
#     --build-arg NEXT_PUBLIC_API_BASE=/api \
#     -t linbit-web .
#
# NOTE: reconstructed in-repo (the server copy was never committed). Ops: diff
# this against the Dockerfile on the server before switching over.
# NEXT_PUBLIC_* values are inlined at BUILD time, so they must be build args;
# changing them requires a rebuild. Empty social URLs = icon hidden.

FROM node:20-alpine AS base
ENV PNPM_HOME=/pnpm PATH=/pnpm:$PATH NEXT_TELEMETRY_DISABLED=1
RUN corepack enable && corepack prepare pnpm@10.34.6 --activate
WORKDIR /repo

FROM base AS build
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY apps/web/package.json apps/web/
COPY packages/ui/package.json packages/ui/
COPY packages/chart/package.json packages/chart/
COPY packages/api-client/package.json packages/api-client/
RUN pnpm install --frozen-lockfile --filter @linbit/web...
COPY apps/web apps/web
COPY packages packages

ARG NEXT_PUBLIC_SITE_URL=
ARG NEXT_PUBLIC_API_BASE=
ARG NEXT_PUBLIC_WS_URL=
ARG NEXT_PUBLIC_INVITE_BASE_URL=
ARG NEXT_PUBLIC_SOCIAL_X=
ARG NEXT_PUBLIC_SOCIAL_TELEGRAM=
ARG NEXT_PUBLIC_SOCIAL_FACEBOOK=
ARG NEXT_PUBLIC_SOCIAL_DISCORD=
ARG NEXT_PUBLIC_SOCIAL_INSTAGRAM=
ARG NEXT_PUBLIC_SOCIAL_YOUTUBE=
ENV NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL \
    NEXT_PUBLIC_API_BASE=$NEXT_PUBLIC_API_BASE \
    NEXT_PUBLIC_WS_URL=$NEXT_PUBLIC_WS_URL \
    NEXT_PUBLIC_INVITE_BASE_URL=$NEXT_PUBLIC_INVITE_BASE_URL \
    NEXT_PUBLIC_SOCIAL_X=$NEXT_PUBLIC_SOCIAL_X \
    NEXT_PUBLIC_SOCIAL_TELEGRAM=$NEXT_PUBLIC_SOCIAL_TELEGRAM \
    NEXT_PUBLIC_SOCIAL_FACEBOOK=$NEXT_PUBLIC_SOCIAL_FACEBOOK \
    NEXT_PUBLIC_SOCIAL_DISCORD=$NEXT_PUBLIC_SOCIAL_DISCORD \
    NEXT_PUBLIC_SOCIAL_INSTAGRAM=$NEXT_PUBLIC_SOCIAL_INSTAGRAM \
    NEXT_PUBLIC_SOCIAL_YOUTUBE=$NEXT_PUBLIC_SOCIAL_YOUTUBE
RUN pnpm --filter @linbit/web build

# next.config.mjs does not set output:"standalone", so the runtime keeps the
# workspace node_modules and runs `next start`.
FROM base AS run
ENV NODE_ENV=production PORT=3000
COPY --from=build /repo /repo
WORKDIR /repo/apps/web
EXPOSE 3000
CMD ["pnpm", "start"]
