FROM node:24-alpine AS base

WORKDIR /app

RUN apk add --no-cache libc6-compat curl \
    && corepack enable \
    && corepack prepare pnpm@10.30.1 --activate

FROM base AS build

COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY . .
ARG VITE_API_BASE=
ARG VITE_GIT_RUN_NUMBER=local
ARG VITE_GIT_REVISION=unknown
ENV VITE_API_BASE=$VITE_API_BASE
ENV VITE_GIT_RUN_NUMBER=$VITE_GIT_RUN_NUMBER
ENV VITE_GIT_REVISION=$VITE_GIT_REVISION
RUN pnpm run build

FROM node:24-alpine AS runner

WORKDIR /app

ENV NODE_ENV=production

RUN apk add --no-cache curl

COPY --from=build /app/dist ./dist
COPY server.mjs ./server.mjs

EXPOSE 3000

CMD ["node", "server.mjs"]
