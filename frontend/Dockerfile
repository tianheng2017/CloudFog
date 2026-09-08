# 云之雾前端镜像（Nuxt 4.2，14 §1）：构建期产出 .output，运行时仅携带产物 + node。
FROM node:24-alpine AS builder
WORKDIR /app
ENV PNPM_HOME=/pnpm PATH=/pnpm:$PATH
RUN corepack enable && corepack prepare pnpm@12.3.4 --activate
# 依赖层：先装清单（postinstall 会生成 tokens，需一并带上 scripts/）
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY scripts/ ./scripts/
RUN pnpm install --frozen-lockfile
COPY . .
# 构建参数：SSR 直连后端基址与站点基址（canonical/OG），生产部署注入
ARG NUXT_API_SERVER_BASE=http://api:8080
ARG NUXT_PUBLIC_SITE_URL=https://api.cloudfog.example
ENV NUXT_API_SERVER_BASE=$NUXT_API_SERVER_BASE \
    NUXT_PUBLIC_SITE_URL=$NUXT_PUBLIC_SITE_URL
RUN pnpm build

FROM node:24-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
COPY --from=builder --chown=node:node /app/.output ./.output
USER node
EXPOSE 3000
# Nitro 产物启动（server/index.mjs）；NUXT_* 环境变量在容器启动时注入即可覆盖构建期值
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:3000/ || exit 1
CMD ["node", ".output/server/index.mjs"]
