# 第一阶段：构建
FROM golang:1.21-alpine AS builder

# 设置工作目录
WORKDIR /app

# 设置Go代理（使用国内镜像加速）
RUN go env -w GOPROXY=https://goproxy.cn,direct

# 复制go.mod和go.sum
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY *.go ./

# 构建应用（简化构建命令）
RUN go build -o app-go .

# 第二阶段：运行
FROM alpine:latest

# 安装必要的运行依赖
RUN apk --no-cache add ca-certificates

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/app-go .

# 创建数据目录
RUN mkdir -p /data

# 暴露端口
EXPOSE 3000 7860

# 运行应用
CMD ["./app-go"]
