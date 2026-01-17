# 使用多阶段构建减小镜像大小

# 第一阶段：构建 Go 应用
FROM golang:1.21-alpine AS builder

# 设置工作目录
WORKDIR /app

# 安装必要的构建工具
RUN apk add --no-cache \
    git \
    make \
    gcc \
    musl-dev \
    upx

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build \
    -a \
    -installsuffix cgo \
    -ldflags="-s -w -extldflags '-static'" \
    -o tunnel-server \
    .

# 压缩二进制文件
RUN upx --brute tunnel-server

# 第二阶段：运行环境
FROM alpine:latest AS runner

# 安装运行时依赖
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    bash \
    shadow \
    su-exec

# 创建非root用户
RUN addgroup -g 1000 tunnel && \
    adduser -u 1000 -G tunnel -D tunnel

# 设置工作目录
WORKDIR /app

# 创建必要的目录
RUN mkdir -p /app/tmp && \
    mkdir -p /app/logs && \
    chown -R tunnel:tunnel /app

# 从构建阶段复制二进制文件
COPY --from=builder --chown=tunnel:tunnel /app/tunnel-server /app/

# 复制配置文件（如果有）
COPY --chown=tunnel:tunnel config.yaml.example /app/
COPY --chown=tunnel:tunnel docker-entrypoint.sh /app/

# 设置环境变量
ENV TZ=Asia/Shanghai \
    FILE_PATH=/app/tmp \
    PORT=3000 \
    EXTERNAL_PORT=7860 \
    UUID=35461c1b-c9fb-efd5-e5d4-cf754d37bd4b

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:${PORT}/daemon-status || exit 1

# 暴露端口
EXPOSE 3000 7860

# 切换用户
USER tunnel

# 设置入口点
ENTRYPOINT ["/app/docker-entrypoint.sh"]

# 启动命令
CMD ["/app/tunnel-server"]
