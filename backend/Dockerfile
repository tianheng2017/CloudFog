# 云之雾后端镜像（10 §3.5 容器基线）：多阶段构建 → 静态二进制 + 极小运行时。
# 单二进制多角色：api / worker / scheduler / migrate（--role= 或子命令切换，01 §2）。
FROM golang:1.27.1-alpine AS builder
WORKDIR /src
# 依赖层单独缓存：仅当 go.mod/go.sum 变化才重新下载
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0：纯静态二进制，可在 alpine/distroless 运行；-trimpath + -s -w 去符号与路径
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cloudfog ./cmd/cloudfog

FROM alpine:3.24
# ca-certificates：上游 HTTPS（OpenAI/Anthropic）必需；tzdata：日志/账期时区（北京时区用固定偏移，仍保留）
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S cloudfog && adduser -S -G cloudfog cloudfog
COPY --from=builder /out/cloudfog /usr/local/bin/cloudfog
# 非 root 运行（08 §9）：二进制无写盘需求
USER cloudfog
WORKDIR /app
EXPOSE 8080
# 存活探测：api 角色提供 /healthz（无依赖）；worker/scheduler 在 compose 中禁用该探测
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["cloudfog"]
CMD ["--role=all"]
