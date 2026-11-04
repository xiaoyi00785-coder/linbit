# syntax=docker/dockerfile:1
#
# Next.js frontend image for the pnpm monorepo: APP=web (default) or APP=partner.
# Build from the repo root:
#   docker build -f deploy/next.Dockerfile --build-arg APP=web \
#     --build-arg NEXT_PUBLIC_NETWORK=mainnet \
#     --build-arg API_PROXY_TARGET=http://gateway:8080 \   (fallback; runtime
#       GATEWAY_INTERNAL_URL + GATEWAY_EDGE_SECRET take precedence)
#     --build-arg NEXT_PUBLIC_SITE_URL=https://www.cowa.click \
#     --build-arg NEXT_PUBLIC_API_BASE=/api \
#     -t linbit-web .
#   (partner: --build-arg APP=partner -t linbit-partner; listens on 3100, web on 3000)
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
ARG APP=web
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY apps/${APP}/package.json apps/${APP}/
COPY packages/ui/package.json packages/ui/
COPY packages/chart/package.json packages/chart/
COPY packages/api-client/package.json packages/api-client/
RUN pnpm install --frozen-lockfile --filter @linbit/${APP}...
COPY apps/${APP} apps/${APP}
COPY packages packages
COPY scripts scripts
COPY apps/web apps/web
COPY apps/partner apps/partner

ARG API_PROXY_TARGET=
ARG NEXT_PUBLIC_NETWORK=
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
# Support contacts (optional; unset ⇒ hidden in the support panel)
ARG NEXT_PUBLIC_SUPPORT_URL=
ARG NEXT_PUBLIC_SUPPORT_TELEGRAM=
ARG NEXT_PUBLIC_SUPPORT_EMAIL=
ENV API_PROXY_TARGET=$API_PROXY_TARGET \
    NEXT_PUBLIC_NETWORK=$NEXT_PUBLIC_NETWORK \
    NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL \
    NEXT_PUBLIC_API_BASE=$NEXT_PUBLIC_API_BASE \
    NEXT_PUBLIC_WS_URL=$NEXT_PUBLIC_WS_URL \
    NEXT_PUBLIC_INVITE_BASE_URL=$NEXT_PUBLIC_INVITE_BASE_URL \
    NEXT_PUBLIC_SOCIAL_X=$NEXT_PUBLIC_SOCIAL_X \
    NEXT_PUBLIC_SOCIAL_TELEGRAM=$NEXT_PUBLIC_SOCIAL_TELEGRAM \
    NEXT_PUBLIC_SOCIAL_FACEBOOK=$NEXT_PUBLIC_SOCIAL_FACEBOOK \
    NEXT_PUBLIC_SOCIAL_DISCORD=$NEXT_PUBLIC_SOCIAL_DISCORD \
    NEXT_PUBLIC_SOCIAL_INSTAGRAM=$NEXT_PUBLIC_SOCIAL_INSTAGRAM \
    NEXT_PUBLIC_SOCIAL_YOUTUBE=$NEXT_PUBLIC_SOCIAL_YOUTUBE \
    NEXT_PUBLIC_SUPPORT_URL=$NEXT_PUBLIC_SUPPORT_URL \
    NEXT_PUBLIC_SUPPORT_TELEGRAM=$NEXT_PUBLIC_SUPPORT_TELEGRAM \
    NEXT_PUBLIC_SUPPORT_EMAIL=$NEXT_PUBLIC_SUPPORT_EMAIL
RUN pnpm --filter @linbit/${APP} build

# next.config.mjs does not set output:"standalone", so the runtime keeps the
# workspace node_modules and runs `next start`.
FROM base AS run
ARG APP=web
ARG API_PROXY_TARGET=
# /gw is a runtime route handler (app/gw/[...path]/route.ts): it prefers
# GATEWAY_INTERNAL_URL (+ GATEWAY_EDGE_SECRET) set on the container at runtime,
# falling back to API_PROXY_TARGET. ipv4first avoids IPv6 ENETUNREACH hangs.
ENV NODE_ENV=production APP=${APP} API_PROXY_TARGET=$API_PROXY_TARGET \
    NODE_OPTIONS=--dns-result-order=ipv4first
COPY --from=build /repo /repo
WORKDIR /repo/apps/${APP}
EXPOSE 3000 3100
CMD ["pnpm", "start"]
