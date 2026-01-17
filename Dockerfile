# 使用多阶段构建支持多架构
# 第一阶段：构建
FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS builder

# 安装构建工具
RUN apk add --no-cache git ca-certificates tzdata

# 设置工作目录
WORKDIR /app

# 设置Go代理（如果需要）
# RUN go env -w GOPROXY=https://goproxy.cn,direct

# 复制依赖文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 设置构建参数
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=latest
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

# 构建应用
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -a -installsuffix cgo \
    -ldflags "-X main.version=${VERSION} \
              -X main.commit=${COMMIT} \
              -X main.buildTime=${BUILD_TIME}" \
    -o app-go .

# 第二阶段：运行
FROM alpine:latest

# 安装必要的运行依赖
RUN apk add --no-cache ca-certificates tzdata curl \
    && addgroup -S appgroup \
    && adduser -S appuser -G appgroup

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder --chown=appuser:appgroup /app/app-go /app/
COPY --from=builder --chown=appuser:appgroup /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# 创建数据目录
RUN mkdir -p /data && chown -R appuser:appgroup /data

# 切换到非root用户
USER appuser

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3000/api/health || exit 1

# 暴露端口
EXPOSE 3000 7860

# 设置环境变量
ENV APP_NAME="app-go" \
    APP_VERSION="1.0.0" \
    FILE_PATH="/data" \
    PORT=3000 \
    EXTERNAL_PORT=7860 \
    TZ=UTC

# 启动命令
ENTRYPOINT ["/app/app-go"]
