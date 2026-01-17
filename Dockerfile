# 构建阶段
FROM golang:1.21-alpine AS builder

# 安装必要的工具
RUN apk add --no-cache git ca-certificates build-base

# 设置工作目录
WORKDIR /app

# 复制go.mod文件
COPY go.mod .

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 编译应用程序
RUN CGO_ENABLED=0 GOOS=linux go build -o proxy-server .

# 运行阶段
FROM alpine:latest

# 安装必要的工具和时区
RUN apk add --no-cache ca-certificates curl bash tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    rm -rf /var/cache/apk/*

# 创建非root用户
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# 创建应用程序目录
RUN mkdir -p /app/tmp && chown -R appuser:appgroup /app

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/proxy-server .

# 切换用户
USER appuser

# 暴露端口
EXPOSE 3000 7860 3001

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:3000/daemon-status || exit 1

# 设置环境变量默认值
ENV SERVER_PORT=3000 \
    EXTERNAL_PORT=7860 \
    FILE_PATH=/app/tmp \
    SUB_PATH=sub \
    UUID=35461c1b-c9fb-efd5-e5d4-cf754d37bd4b \
    NEZHA_SERVER=gwwjllhldpjy.us-west-1.clawcloudrun.com:443 \
    NEZHA_KEY=rRA5ZrgOmsosl7EiyIuJBhnGwcAqWDUr \
    ARGO_DOMAIN=hug2.goyo123.ip-ddns.com \
    ARGO_AUTH='{"AccountTag":"07df2fb1e52e8a732d40a84b93c277f9","TunnelSecret":"15ZYg4Y6Mr9tlVyw7IqR1ks32AwpOu4p2P9F5+5COSk=","TunnelID":"c4e5c285-03df-4aa3-a3d1-1f76b617123f","Endpoint":""}' \
    ARGO_PORT=7860 \
    CFIP=cdns.doon.eu.org \
    CFPORT=443 \
    DAEMON_CHECK_INTERVAL=30000 \
    DAEMON_MAX_RETRIES=5 \
    DAEMON_RESTART_DELAY=10000

# 启动应用程序
CMD ["./proxy-server"]
