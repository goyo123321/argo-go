# 使用多阶段构建减小镜像大小
# 第一阶段：构建 Go 应用
FROM golang:1.21-alpine AS builder

# 安装必要的构建工具
RUN apk add --no-cache git build-base

# 设置工作目录
WORKDIR /app

# 复制 Go 模块文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 编译应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o proxy-server main.go

# 第二阶段：运行阶段
FROM alpine:latest

# 安装必要的运行时依赖
RUN apk --no-cache add ca-certificates curl bash \
    && update-ca-certificates \
    && rm -rf /var/cache/apk/*

# 创建非root用户
RUN addgroup -g 1001 -S appuser && \
    adduser -u 1001 -S appuser -G appuser

# 创建工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/proxy-server /app/proxy-server
COPY --from=builder /app/index.html /app/index.html 2>/dev/null || true

# 创建必要的目录
RUN mkdir -p /tmp/app && chown -R appuser:appuser /tmp/app /app

# 切换到非root用户
USER appuser

# 暴露端口
EXPOSE 3000 7860 3001

# 设置容器启动命令
CMD ["/app/proxy-server"]
