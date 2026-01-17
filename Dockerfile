# 构建阶段
FROM golang:1.19-alpine AS builder

# 安装必要的工具
RUN apk add --no-cache git ca-certificates

# 设置工作目录
WORKDIR /app

# 复制go.mod和go.sum文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 嵌入index.html文件
COPY index.html .

# 编译应用程序
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o proxy-server .

# 运行阶段
FROM alpine:latest

# 安装必要的工具
RUN apk add --no-cache ca-certificates curl bash tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone

# 创建非root用户
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# 创建应用程序目录
RUN mkdir -p /app/tmp && chown -R appuser:appgroup /app

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/proxy-server .
COPY --from=builder /app/index.html .

# 复制时区文件
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# 切换用户
USER appuser

# 暴露端口
EXPOSE 3000 7860 3001

# 启动应用程序
CMD ["./proxy-server"]
